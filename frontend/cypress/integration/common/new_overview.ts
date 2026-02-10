import { Given, Then, When } from '@badeball/cypress-cucumber-preprocessor';

const CONTROL_PLANES_API_PATHNAME = '**/api/mesh/controlplanes';
const ISTIO_CONFIGS_API_PATHNAME = '**/api/istio/config';
const SERVICE_LATENCIES_API_PATHNAME = '**/api/overview/metrics/services/latency';
const SERVICE_RATES_API_PATHNAME = '**/api/overview/metrics/services/rates';

let shouldWaitControlPlanesRetry = false;
let shouldWaitServiceInsightsRetry = false;
let didServiceInsightsRetry = false;
let lastClickedServiceInsightsHref: string | undefined;

const getControlPlanesCard = (): Cypress.Chainable => {
  return cy.contains('Control planes').closest('div[data-ouia-component-type="PF6/Card"]');
};

const getDataPlanesCard = (): Cypress.Chainable => {
  return cy.contains('Data planes').closest('div[data-ouia-component-type="PF6/Card"]');
};

const getServiceInsightsCard = (): Cypress.Chainable => {
  return cy.getBySel('service-insights-card');
};

const getIstioConfigsCard = (): Cypress.Chainable => {
  return cy.contains('Istio configs').closest('div[data-ouia-component-type="PF6/Card"]');
};

const parseUrlPathAndSearch = (href: string): string => {
  try {
    const u = new URL(href, Cypress.config('baseUrl') as string);
    return `${u.pathname}${u.search}`;
  } catch {
    return href;
  }
};

Given('Istio configs API is observed', () => {
  cy.intercept({ method: 'GET', pathname: ISTIO_CONFIGS_API_PATHNAME }).as('allIstioConfigs');
});

When('user clicks View Istio config in Istio configs card', () => {
  getIstioConfigsCard().within(() => {
    cy.contains('button', 'View Istio config').should('be.visible').click();
  });
});

Then('user is redirected to Istio config list with all namespaces', () => {
  cy.location('pathname').should('match', /\/console\/istio$/);

  cy.location('search').then(search => {
    const params = new URLSearchParams(search);
    const urlNamespaces = Array.from(
      new Set(
        (params.get('namespaces') ?? '')
          .split(',')
          .map(n => n.trim())
          .filter(Boolean)
      )
    ).sort();

    expect(urlNamespaces.length, 'namespaces query param should be present').to.be.greaterThan(0);

    cy.request('api/namespaces').then(resp => {
      const allNamespaces = Array.from(new Set((resp.body as Array<{ name: string }>).map(ns => ns.name))).sort();
      expect(urlNamespaces).to.deep.eq(allNamespaces);
    });
  });
});

Given('Control planes API is observed', () => {
  cy.intercept({ method: 'GET', pathname: CONTROL_PLANES_API_PATHNAME }).as('controlPlanes');
});

Given('Control planes API responds slowly', () => {
  cy.intercept(
    {
      method: 'GET',
      pathname: CONTROL_PLANES_API_PATHNAME
    },
    req => {
      req.continue(res => {
        res.delay = 2000;
      });
    }
  ).as('controlPlanes');
});

Given('Control planes API fails', () => {
  cy.intercept(
    {
      method: 'GET',
      pathname: CONTROL_PLANES_API_PATHNAME
    },
    {
      statusCode: 500,
      body: {}
    }
  ).as('controlPlanes');
});

Given('Control planes API fails once', () => {
  shouldWaitControlPlanesRetry = true;

  // Observe the retry call (passthrough).
  cy.intercept({ method: 'GET', pathname: CONTROL_PLANES_API_PATHNAME }).as('controlPlanesRetry');

  // Fail only the first call (to show error state).
  cy.intercept(
    {
      method: 'GET',
      pathname: CONTROL_PLANES_API_PATHNAME,
      times: 1
    },
    {
      statusCode: 500,
      body: {}
    }
  ).as('controlPlanes');
});

Then('Control planes card shows loading state without count or footer link', () => {
  // Ensure we are still in loading state (don't wait for the response).
  getControlPlanesCard().within(() => {
    cy.contains('Fetching control plane data').should('be.visible');
    cy.contains('Control planes (').should('not.exist');
    cy.contains('View Control planes').should('not.exist');
  });
});

Then('Control planes card shows error state without count or footer link', () => {
  cy.wait('@controlPlanes');
  getControlPlanesCard().within(() => {
    cy.contains('Control planes could not be loaded').should('be.visible');
    cy.contains('Try Again').should('be.visible');
    cy.contains('Control planes (').should('not.exist');
    cy.contains('View Control planes').should('not.exist');
  });
});

When('user clicks Try Again in Control planes card', () => {
  getControlPlanesCard().within(() => {
    cy.contains('button', 'Try Again').should('be.visible').click();
  });
  if (shouldWaitControlPlanesRetry) {
    cy.wait('@controlPlanesRetry');
    shouldWaitControlPlanesRetry = false;
  }
});

Then('Control planes card shows count and footer link', () => {
  getControlPlanesCard().within(() => {
    cy.contains('Control planes (').should('be.visible');
    cy.getBySel('control-planes-view-namespaces').should('be.visible');
  });
});

When('user navigates to Namespaces page from Control planes card', () => {
  cy.wait('@controlPlanes');

  // Navigation should always be via the footer link (Namespaces with type filter).
  getControlPlanesCard().within(() => {
    cy.getBySel('control-planes-view-namespaces').should('be.visible').click();
  });
});

Then('user is redirected to Namespaces page with control-plane type filter', () => {
  cy.location('pathname').should('match', /\/(console|ossmconsole)\/namespaces$/);
  cy.location('search').then(search => {
    const params = new URLSearchParams(search);
    expect(params.get('type')).to.eq('Control plane');
  });
});

Given('Service insights APIs are observed', () => {
  didServiceInsightsRetry = false;
  cy.intercept({ method: 'GET', pathname: SERVICE_LATENCIES_API_PATHNAME }).as('serviceLatencies');
  cy.intercept({ method: 'GET', pathname: SERVICE_RATES_API_PATHNAME }).as('serviceRates');
});

Given('Service insights APIs respond slowly', () => {
  didServiceInsightsRetry = false;
  cy.intercept({ method: 'GET', pathname: SERVICE_LATENCIES_API_PATHNAME }, req => {
    req.continue(res => {
      res.delay = 2000;
    });
  }).as('serviceLatencies');

  cy.intercept({ method: 'GET', pathname: SERVICE_RATES_API_PATHNAME }, req => {
    req.continue(res => {
      res.delay = 2000;
    });
  }).as('serviceRates');
});

Given('Service insights APIs fail', () => {
  didServiceInsightsRetry = false;
  cy.intercept({ method: 'GET', pathname: SERVICE_LATENCIES_API_PATHNAME }, { statusCode: 500, body: {} }).as(
    'serviceLatencies'
  );
  cy.intercept({ method: 'GET', pathname: SERVICE_RATES_API_PATHNAME }, { statusCode: 500, body: {} }).as(
    'serviceRates'
  );
});

Given('Service insights APIs fail once', () => {
  didServiceInsightsRetry = false;
  shouldWaitServiceInsightsRetry = true;

  // Observe subsequent (retry) calls without modifying them.
  cy.intercept({ method: 'GET', pathname: SERVICE_LATENCIES_API_PATHNAME }).as('serviceLatencies');
  cy.intercept({ method: 'GET', pathname: SERVICE_RATES_API_PATHNAME }).as('serviceRates');

  cy.intercept({ method: 'GET', pathname: SERVICE_LATENCIES_API_PATHNAME, times: 1 }, { statusCode: 500, body: {} }).as(
    'serviceLatenciesFailOnce'
  );

  cy.intercept({ method: 'GET', pathname: SERVICE_RATES_API_PATHNAME, times: 1 }, { statusCode: 500, body: {} }).as(
    'serviceRatesFailOnce'
  );
});

Then('Service insights card shows loading state without tables or footer link', () => {
  getServiceInsightsCard().within(() => {
    cy.contains('Fetching service data').should('be.visible');
    cy.getBySel('service-insights-view-all-services').should('not.exist');
  });
});

Then('Service insights card shows error state without tables or footer link', () => {
  if (shouldWaitServiceInsightsRetry) {
    cy.wait('@serviceLatenciesFailOnce');
    cy.wait('@serviceRatesFailOnce');
  } else {
    cy.wait('@serviceLatencies');
    cy.wait('@serviceRates');
  }

  getServiceInsightsCard().within(() => {
    cy.contains('Failed to load service data').should('be.visible');
    cy.contains('button', 'Try Again').should('be.visible');
    cy.getBySel('service-insights-view-all-services').should('not.exist');
  });
});

When('user clicks Try Again in Service insights card', () => {
  getServiceInsightsCard().within(() => {
    cy.contains('button', 'Try Again').should('be.visible').click();
  });

  if (shouldWaitServiceInsightsRetry) {
    cy.wait('@serviceLatencies');
    cy.wait('@serviceRates');
    didServiceInsightsRetry = true;
    shouldWaitServiceInsightsRetry = false;
  }
});

Then('Service insights card shows data tables and footer link', () => {
  // If we didn't already wait for a retry, wait for the initial real API responses so assertions are stable.
  if (!didServiceInsightsRetry) {
    cy.wait('@serviceLatencies');
    cy.wait('@serviceRates');
  }

  getServiceInsightsCard().within(() => {
    cy.contains('Fetching service data').should('not.exist');
    cy.contains('Failed to load service data').should('not.exist');
    cy.getBySel('service-insights-view-all-services').should('be.visible');
  });

  // Rates section: either a table with headers/rows or an explicit "No data" message.
  cy.getBySel('service-insights-rates').within(() => {
    cy.get('table').then($table => {
      if ($table.length === 0) {
        cy.contains('No data').should('be.visible');
        return;
      }

      cy.contains('th', 'Name').should('be.visible');
      cy.contains('th', 'Errors').should('be.visible');

      cy.get('tbody tr').then($rows => {
        if ($rows.length === 0) {
          return;
        }
        cy.wrap($rows[0]).within(() => {
          cy.get('a')
            .should('have.attr', 'href')
            .and('match', /\/namespaces\/.+\/services\/.+/);
          cy.contains('%').should('be.visible');
        });
      });
    });
  });

  // Latencies section: either a table with headers/rows or an explicit "No data" message.
  cy.getBySel('service-insights-latencies').within(() => {
    cy.get('table').then($table => {
      if ($table.length === 0) {
        cy.contains('No data').should('be.visible');
        return;
      }

      cy.contains('th', 'Name').should('be.visible');
      cy.contains('th', 'Latency').should('be.visible');

      cy.get('tbody tr').then($rows => {
        if ($rows.length === 0) {
          return;
        }
        cy.wrap($rows[0]).within(() => {
          cy.get('a')
            .should('have.attr', 'href')
            .and('match', /\/namespaces\/.+\/services\/.+/);
          cy.contains(/ms|s/).should('be.visible');
        });
      });
    });
  });
});

When('user clicks View all services in Service insights card', () => {
  cy.wait('@serviceLatencies');
  cy.wait('@serviceRates');

  getServiceInsightsCard().within(() => {
    cy.getBySel('service-insights-view-all-services').should('be.visible').click();
  });
});

Then('user is redirected to Services list with all namespaces and service insights sorting', () => {
  cy.location('pathname').should('match', /\/(console|ossmconsole)\/services$/);
  cy.location('search').then(search => {
    const params = new URLSearchParams(search);

    // Filters from the "View all" navigation
    expect(params.get('direction')).to.eq('asc');
    expect(params.get('sort')).to.eq('he');

    const urlNamespaces = Array.from(
      new Set(
        (params.get('namespaces') ?? '')
          .split(',')
          .map(n => n.trim())
          .filter(Boolean)
      )
    ).sort();

    expect(urlNamespaces.length, 'namespaces query param should be present').to.be.greaterThan(0);

    cy.request('api/namespaces').then(resp => {
      const allNamespaces = Array.from(new Set((resp.body as Array<{ name: string }>).map(ns => ns.name))).sort();
      expect(urlNamespaces).to.deep.eq(allNamespaces);
    });
  });
});

When('user clicks a valid service link in Service insights card', () => {
  cy.wait('@serviceLatencies');
  cy.wait('@serviceRates');

  lastClickedServiceInsightsHref = undefined;

  // Wait until the card is done rendering after the API responses:
  // it must have either at least one service link, or show the empty state.
  getServiceInsightsCard()
    .should($card => {
      const hasServiceLink =
        $card.find('[data-test="service-insights-rates"] a').length > 0 ||
        $card.find('[data-test="service-insights-latencies"] a').length > 0;

      const hasEmptyState = $card.text().includes('No data');

      expect(
        hasServiceLink || hasEmptyState,
        'Service Insights should eventually show at least one service link or the empty state'
      ).to.eq(true);
    })
    .then($card => {
      const hasRateLink = $card.find('[data-test="service-insights-rates"] a').length > 0;
      const hasLatencyLink = $card.find('[data-test="service-insights-latencies"] a').length > 0;
      const hasServiceLink = hasRateLink || hasLatencyLink;

      if (!hasServiceLink) {
        // Nothing to click in real data; assert the empty state and exit.
        getServiceInsightsCard().within(() => {
          cy.contains('No data').should('be.visible');
        });
        return;
      }

      const containerSel = hasRateLink
        ? '[data-test="service-insights-rates"]'
        : '[data-test="service-insights-latencies"]';

      const isServiceDetailsPageValid = ($body: JQuery<HTMLBodyElement>): boolean => {
        // When the service is not found, ServiceDetailsPage sets error and does NOT render tabs.
        return $body.find('#basic-tabs').length > 0;
      };

      const escapeCssAttrValue = (value: string): string => value.replace(/\\/g, '\\\\').replace(/"/g, '\\"');

      const tryHrefAtIndex = (hrefs: string[], idx: number): Cypress.Chainable => {
        if (idx >= hrefs.length) {
          throw new Error('No valid Service Insights service link found (all navigations ended in an error page).');
        }

        const href = hrefs[idx];
        lastClickedServiceInsightsHref = parseUrlPathAndSearch(href);

        // Click inside the Service Insights card to avoid matching other links.
        return getServiceInsightsCard()
          .within(() => {
            cy.get(`a[href="${escapeCssAttrValue(href)}"]`)
              .first()
              .should('be.visible')
              .click();
          })
          .then(() => cy.get('#loading_kiali_spinner', { timeout: 40000 }).should('not.exist'))
          .then(() => cy.get('body', { timeout: 40000 }))
          .then($body => {
            if (isServiceDetailsPageValid($body as JQuery<HTMLBodyElement>)) {
              return;
            }

            // Invalid service details; go back and try the next link.
            return cy
              .go('back')
              .then(() => cy.get('#loading_kiali_spinner', { timeout: 40000 }).should('not.exist'))
              .then(() => cy.location('pathname').should('match', /\/(console|ossmconsole)\/overview$/))
              .then(() => getServiceInsightsCard().should('be.visible'))
              .then(() => tryHrefAtIndex(hrefs, idx + 1));
          });
      };

      cy.get(`${containerSel} table tbody tr a`)
        .should('exist')
        .then($links => {
          const hrefs = Array.from($links)
            .map(a => (a as HTMLAnchorElement).getAttribute('href') ?? '')
            .map(h => h.trim())
            .filter(Boolean);

          const uniqueHrefs = Array.from(new Set(hrefs));
          return tryHrefAtIndex(uniqueHrefs, 0);
        });
    });
});

Then('user is redirected to that Service details page', () => {
  if (!lastClickedServiceInsightsHref) {
    // No data case: the previous step already asserted "No data".
    return;
  }

  const normalizePath = (pathname: string): string => {
    return pathname.replace(/^\/(console|ossmconsole)/, '');
  };

  const toUrl = (pathAndSearch: string): URL => {
    return new URL(pathAndSearch, Cypress.config('baseUrl') as string);
  };

  cy.location('pathname').then(pathname => {
    cy.location('search').then(search => {
      const actualUrl = toUrl(`${pathname}${search}`);
      const expectedUrl = toUrl(lastClickedServiceInsightsHref);

      // Path must match exactly (ignoring /console vs /ossmconsole prefix).
      expect(normalizePath(actualUrl.pathname)).to.eq(normalizePath(expectedUrl.pathname));

      // Expected query params must be present (allowing extra params like duration/refresh).
      expectedUrl.searchParams.forEach((value, key) => {
        expect(actualUrl.searchParams.get(key), `query param ${key}`).to.eq(value);
      });
    });
  });

  // Basic smoke validation that the page exists/loaded.
  cy.get('#basic-tabs').should('exist');
  cy.contains('button, a', 'Overview').should('be.visible');
  cy.contains('button, a', 'Traffic').should('be.visible');
  cy.contains('button, a', 'Inbound Metrics').should('be.visible');
});

When('user clicks View Data planes in Data planes card', () => {
  getDataPlanesCard().within(() => {
    cy.getBySel('data-planes-view-namespaces').should('be.visible').click();
  });
});

Then('user is redirected to Namespaces page with data-plane type filter', () => {
  cy.location('pathname').should('match', /\/console\/namespaces$/);
  cy.location('search').then(search => {
    const params = new URLSearchParams(search);
    expect(params.get('type')).to.eq('Data plane');
  });
});
