/*
  Shared utilities for managing Kiali configuration in Cypress tests.
  These functions allow tests to enable/disable Kiali features by modifying
  the Kiali CR (for operator installations) or ConfigMap (for Helm installations).
*/

// Type definition for Kiali runtime info
export interface KialiRuntimeInfo {
  configMapName: string;
  deploymentName: string;
  namespace: string;
}

/**
 * Discovers Kiali runtime information (deployment name, namespace, configmap name).
 * Supports both operator-managed and Helm installations.
 */
export const discoverKialiRuntimeInfo = (): Cypress.Chainable<KialiRuntimeInfo> => {
  const resolveConfigMap = (namespace: string, deploymentName: string): Cypress.Chainable<KialiRuntimeInfo> => {
    return cy
      .exec(
        `kubectl get deployment/${deploymentName} -n ${namespace} -o jsonpath="{.spec.template.spec.volumes[?(@.configMap)].configMap.name}"`,
        { failOnNonZeroExit: false }
      )
      .then(result => {
        // The jsonpath may return multiple configmap names (space-separated)
        // We need to find the one that contains config.yaml
        const configMapCandidates = result.stdout.trim().split(/\s+/).filter(Boolean);

        if (configMapCandidates.length === 0) {
          return cy.wrap({ configMapName: 'kiali', deploymentName, namespace });
        }

        if (configMapCandidates.length === 1) {
          return cy.wrap({ configMapName: configMapCandidates[0], deploymentName, namespace });
        }

        // Multiple configmaps - find the one with config.yaml
        const findConfigMapWithConfigYaml = (idx: number): Cypress.Chainable<string> => {
          if (idx >= configMapCandidates.length) {
            // Fallback to first candidate or 'kiali'
            return cy.wrap(configMapCandidates[0] || 'kiali');
          }

          const cmName = configMapCandidates[idx];
          return cy
            .exec(`kubectl get configmap ${cmName} -n ${namespace} -o jsonpath="{.data.config\\\\.yaml}"`, {
              failOnNonZeroExit: false
            })
            .then(cmRes => {
              if (cmRes.code === 0 && cmRes.stdout.trim() !== '') {
                return cy.wrap(cmName);
              }
              return findConfigMapWithConfigYaml(idx + 1);
            });
        };

        return findConfigMapWithConfigYaml(0).then(configMapName => ({
          configMapName,
          deploymentName,
          namespace
        }));
      });
  };

  return cy
    .exec(
      'kubectl get deployments -A -l app.kubernetes.io/name=kiali -o=custom-columns=NS:.metadata.namespace,NAME:.metadata.name --no-headers',
      { failOnNonZeroExit: false }
    )
    .then(result => {
      const lines = result.stdout
        .trim()
        .split('\n')
        .map(l => l.trim())
        .filter(Boolean);

      if (!lines.length || lines[0] === '') {
        // Fallback: look for a deployment named "kiali" in any namespace.
        return cy
          .exec(
            'kubectl get deployments -A -o=custom-columns=NS:.metadata.namespace,NAME:.metadata.name --no-headers',
            {
              failOnNonZeroExit: false
            }
          )
          .then(fallbackResult => {
            const fallbackLines = fallbackResult.stdout
              .split('\n')
              .map(l => {
                const parts = l.trim().split(/\s+/);
                return parts.length >= 2 ? `${parts[0]}/${parts[1]}` : '';
              })
              .filter(l => l.includes('/kiali'))
              .filter(Boolean);

            const preferredNamespaces = ['istio-system', 'kiali-operator', 'default'];

            let fallbackChosen: string | undefined;
            for (const ns of preferredNamespaces) {
              fallbackChosen = fallbackLines.find(l => l.startsWith(`${ns}/`));
              if (fallbackChosen) {
                break;
              }
            }
            fallbackChosen = fallbackChosen ?? fallbackLines[0];

            if (!fallbackChosen) {
              throw new Error(
                'Unable to locate Kiali deployment. Tried label app.kubernetes.io/name=kiali and fallback deployment named "kiali" in any namespace.'
              );
            }

            const [namespace, deploymentName] = fallbackChosen.split('/');
            return resolveConfigMap(namespace, deploymentName);
          });
      }

      // Parse the first line: "NAMESPACE   DEPLOYMENT_NAME"
      const parts = lines[0].split(/\s+/);
      const namespace = parts[0];
      const deploymentName = parts[1];
      return resolveConfigMap(namespace, deploymentName);
    });
};

/**
 * Restarts the Kiali deployment and waits for it to be ready.
 */
export const restartKiali = (deploymentName: string, namespace: string): void => {
  cy.exec(
    `kubectl rollout restart deployment/${deploymentName} -n ${namespace} && kubectl rollout status deployment/${deploymentName} -n ${namespace} --timeout=240s`,
    { timeout: 300000 }
  );
};

/**
 * Configuration for setting a boolean Kiali option.
 */
export interface KialiBooleanConfig {
  // The yq path to the config option (e.g., '.external_services.tracing.use_waypoint_name')
  configPath: string;
  // The CR spec path for operator installations (e.g., 'external_services.tracing.use_waypoint_name')
  crSpecPath: string;
}

/**
 * Configuration for enabling a Kiali feature.
 */
export interface KialiFeatureConfig extends KialiBooleanConfig {
  // Cypress env key to store the previous value
  envKeyPrev: string;
}

const buildNestedPatchObject = (path: string, value: unknown): Record<string, unknown> => {
  const pathParts = path.split('.');
  let patchObj: unknown = value;
  for (let i = pathParts.length - 1; i >= 0; i--) {
    patchObj = { [pathParts[i]]: patchObj };
  }
  return patchObj as Record<string, unknown>;
};

const normalizeBooleanOutput = (rawValue: string): string => {
  return rawValue.trim() === 'true' ? 'true' : 'false';
};

const persistKialiRuntimeEnv = (info: KialiRuntimeInfo): void => {
  Cypress.env('KIALI_CONFIGMAP_NAME', info.configMapName);
  Cypress.env('KIALI_DEPLOYMENT_NAME', info.deploymentName);
  Cypress.env('KIALI_DEPLOYMENT_NAMESPACE', info.namespace);
};

type UpdateKialiBooleanOptions = {
  envKeyPrev?: string;
};

/**
 * Shared helper to set a boolean Kiali option and restart Kiali only when the
 * value actually changes. Supports both operator (Kiali CR) and Helm
 * (ConfigMap) installations.
 */
const updateKialiBooleanConfig = (
  config: KialiBooleanConfig,
  value: boolean,
  options: UpdateKialiBooleanOptions = {}
): void => {
  discoverKialiRuntimeInfo().then(info => {
    persistKialiRuntimeEnv(info);
    const desiredValue = value ? 'true' : 'false';
    const doRestart = (): void => {
      restartKiali(info.deploymentName, info.namespace);
    };

    cy.exec(
      `kubectl get deployment/${info.deploymentName} -n ${info.namespace} -o jsonpath="{.metadata.annotations.operator-sdk\\/primary-resource}"`,
      { failOnNonZeroExit: false }
    ).then(result => {
      const primaryResource = result.stdout.trim();
      Cypress.env('KIALI_PRIMARY_RESOURCE', primaryResource);

      if (primaryResource) {
        // Operator installation - patch the Kiali CR
        const [crNamespace, crName] = primaryResource.split('/');
        cy.exec(`kubectl get kiali ${crName} -n ${crNamespace} -o jsonpath="{.spec.${config.crSpecPath}}"`, {
          failOnNonZeroExit: false
        }).then(r => {
          if (options.envKeyPrev) {
            Cypress.env(options.envKeyPrev, r.stdout.trim());
          }
          const currentValue = normalizeBooleanOutput(r.stdout);
          if (currentValue === desiredValue) {
            return;
          }

          const patchJson = JSON.stringify({ spec: buildNestedPatchObject(config.crSpecPath, value) });
          cy.exec(`kubectl patch kiali ${crName} -n ${crNamespace} --type merge -p '${patchJson}'`).then(() =>
            doRestart()
          );
        });
        return;
      }

      // Helm installation - update the ConfigMap
      cy.exec(
        `kubectl get configmap ${info.configMapName} -n ${info.namespace} -o jsonpath="{.data.config\\\\.yaml}" > /tmp/kiali-config.yaml`
      ).then(() => {
        cy.exec(`yq '${config.configPath}' /tmp/kiali-config.yaml`, {
          failOnNonZeroExit: false
        }).then(r => {
          if (options.envKeyPrev) {
            Cypress.env(options.envKeyPrev, r.stdout.trim());
          }
          const currentValue = normalizeBooleanOutput(r.stdout);
          if (currentValue === desiredValue) {
            return;
          }

          cy.exec(`yq -i '${config.configPath} = ${desiredValue}' /tmp/kiali-config.yaml`);
          cy.exec(
            `kubectl create configmap ${info.configMapName} -n ${info.namespace} --from-file=config.yaml=/tmp/kiali-config.yaml -o yaml --dry-run=client | kubectl apply -f -`
          ).then(() => doRestart());
        });
      });
    });
  });
};

/**
 * Sets a boolean Kiali option and restarts Kiali only when the value actually changes.
 */
export const setKialiBooleanConfig = (config: KialiBooleanConfig, value: boolean): void => {
  updateKialiBooleanConfig(config, value);
};

/**
 * Enables a Kiali feature by modifying the configuration.
 * Supports both operator (Kiali CR) and Helm (ConfigMap) installations.
 * Stores the previous value in Cypress env for cleanup.
 */
export const enableKialiFeature = (featureConfig: KialiFeatureConfig): void => {
  updateKialiBooleanConfig(featureConfig, true, { envKeyPrev: featureConfig.envKeyPrev });
};

/**
 * Restores a Kiali feature to its previous value after a test.
 * Call this in an After hook.
 */
export const restoreKialiFeature = (featureConfig: KialiFeatureConfig): void => {
  const primaryResource = (Cypress.env('KIALI_PRIMARY_RESOURCE') as string | undefined) ?? '';
  const prev = (Cypress.env(featureConfig.envKeyPrev) as string | undefined) ?? '';
  const prevBool = prev === 'true';

  const kialiDeploymentName = (Cypress.env('KIALI_DEPLOYMENT_NAME') as string | undefined) ?? 'kiali';
  const kialiDeploymentNamespace = (Cypress.env('KIALI_DEPLOYMENT_NAMESPACE') as string | undefined) ?? 'istio-system';
  const kialiConfigMapName = (Cypress.env('KIALI_CONFIGMAP_NAME') as string | undefined) ?? 'kiali';

  const doRestart = (): void => {
    cy.exec(
      `kubectl rollout restart deployment/${kialiDeploymentName} -n ${kialiDeploymentNamespace} && kubectl rollout status deployment/${kialiDeploymentName} -n ${kialiDeploymentNamespace} --timeout=240s`,
      { timeout: 300000, failOnNonZeroExit: false }
    );
  };

  if (primaryResource) {
    // Operator installation - patch the Kiali CR
    const parts = primaryResource.split('/');
    const crNamespace = parts[0];
    const crName = parts[1];

    // Build the patch JSON dynamically
    const pathParts = featureConfig.crSpecPath.split('.');
    let patchObj: Record<string, unknown> = { enabled: prevBool };
    for (let i = pathParts.length - 2; i >= 0; i--) {
      patchObj = { [pathParts[i]]: patchObj };
    }
    const patchJson = JSON.stringify({ spec: patchObj });

    cy.exec(`kubectl patch kiali ${crName} -n ${crNamespace} --type merge -p '${patchJson}'`, {
      failOnNonZeroExit: false
    }).then(() => doRestart());
    return;
  }

  // Helm installation - restore via ConfigMap
  cy.exec(
    `kubectl get configmap ${kialiConfigMapName} -n ${kialiDeploymentNamespace} -o jsonpath="{.data.config\\\\.yaml}" > /tmp/kiali-config.yaml`,
    { failOnNonZeroExit: false }
  ).then(() => {
    if (prev === '' || prev === 'null') {
      cy.exec(`yq -i 'del(${featureConfig.configPath})' /tmp/kiali-config.yaml`, { failOnNonZeroExit: false });
    } else {
      cy.exec(`yq -i '${featureConfig.configPath} = ${prevBool}' /tmp/kiali-config.yaml`, {
        failOnNonZeroExit: false
      });
    }

    cy.exec(
      `kubectl create configmap ${kialiConfigMapName} -n ${kialiDeploymentNamespace} --from-file=config.yaml=/tmp/kiali-config.yaml -o yaml --dry-run=client | kubectl apply -f -`,
      { failOnNonZeroExit: false }
    ).then(() => doRestart());
  });
};

// Pre-defined feature configurations
export const GRAPH_CACHE_CONFIG: KialiFeatureConfig = {
  configPath: '.kiali_internal.graph_cache.enabled',
  crSpecPath: 'kiali_internal.graph_cache.enabled',
  envKeyPrev: 'GRAPH_CACHE_PREV'
};

export const HEALTH_CACHE_CONFIG: KialiFeatureConfig = {
  configPath: '.kiali_internal.health_cache.enabled',
  crSpecPath: 'kiali_internal.health_cache.enabled',
  envKeyPrev: 'HEALTH_CACHE_PREV'
};

export const TRACING_USE_WAYPOINT_NAME_CONFIG: KialiBooleanConfig = {
  configPath: '.external_services.tracing.use_waypoint_name',
  crSpecPath: 'external_services.tracing.use_waypoint_name'
};
