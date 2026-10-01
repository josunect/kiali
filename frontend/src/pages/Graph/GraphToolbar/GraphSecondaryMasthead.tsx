import * as React from 'react';
import { SecondaryMasthead } from 'components/Nav/SecondaryMasthead';
import { NamespaceDropdown } from 'components/Dropdown/NamespaceDropdown';
import { kialiStyle } from 'styles/StyleUtils';
import { TourStop } from 'components/Tour/TourStop';
import { GraphTourStops } from '../GraphHelpTour';
import { ToolbarDropdown } from 'components/Dropdown/ToolbarDropdown';
import { GraphType, TelemetryVendor } from 'types/Graph';
import { capitalize, findKey, mapValues, startCase } from 'lodash-es';
import { TimeDurationComponent } from '../../../components/Time/TimeDurationComponent';
import { GraphTraffic } from './GraphTraffic';
import { t } from 'utils/I18nUtils';

type GraphSecondaryMastheadProps = {
  disabled: boolean;
  graphType: GraphType;
  isNodeGraph: boolean;
  onGraphTypeChange: (graphType: GraphType) => void;
  onTelemetryVendorChange: (vendor: TelemetryVendor) => void;
  telemetryVendor: TelemetryVendor;
  tracingEnabled: boolean;
};

const leftSpacerStyle = kialiStyle({
  marginLeft: '0.5rem'
});

const vrStyle = kialiStyle({
  border: '1px inset',
  height: '1.25rem',
  margin: '0.25rem 0 0 0.5rem',
  width: '1px'
});

const rightToolbarStyle = kialiStyle({
  float: 'right'
});

/**
 *  Key-value pair object representation of GraphType enum.  Values are human-readable versions of enum keys.
 *
 *  Example:  GraphType => {'APP': 'App', 'VERSIONED_APP': 'VersionedApp'}
 */
const GRAPH_TYPES = mapValues(GraphType, val => `${capitalize(startCase(val))} graph`);

export const GraphSecondaryMasthead: React.FC<GraphSecondaryMastheadProps> = (props: GraphSecondaryMastheadProps) => {
  const telemetryVendors = {
    ISTIO: t('Metrics map'),
    TRACING: t('Traces map')
  };

  const setGraphType = (type: string): void => {
    const graphType: GraphType = GraphType[type] as GraphType;
    if (props.graphType !== graphType) {
      props.onGraphTypeChange(graphType);
    }
  };

  const setTelemetryVendor = (vendorKey: string): void => {
    const vendor: TelemetryVendor = TelemetryVendor[vendorKey] as TelemetryVendor;
    if (props.telemetryVendor !== vendor) {
      props.onTelemetryVendorChange(vendor);
    }
  };

  const graphTypeKey = findKey(GraphType, val => val === props.graphType)!;
  const telemetryVendorKey = findKey(TelemetryVendor, val => val === props.telemetryVendor)!;

  const vendorOptions = props.tracingEnabled ? telemetryVendors : { ISTIO: telemetryVendors.ISTIO };

  return (
    <SecondaryMasthead>
      <>
        <NamespaceDropdown disabled={props.isNodeGraph} />

        <span className={vrStyle} />

        <TourStop info={GraphTourStops.GraphTraffic}>
          <span className={leftSpacerStyle}>
            <GraphTraffic disabled={props.disabled} />
          </span>
        </TourStop>

        <span className={vrStyle} />

        <span className={leftSpacerStyle}>
          <ToolbarDropdown
            id={'graph_telemetry_vendor_dropdown'}
            disabled={props.disabled || props.isNodeGraph || !props.tracingEnabled}
            handleSelect={setTelemetryVendor}
            value={telemetryVendorKey}
            label={telemetryVendors[telemetryVendorKey]}
            options={vendorOptions}
          />
        </span>

        <span className={vrStyle} />

        <TourStop info={GraphTourStops.GraphType}>
          <span className={leftSpacerStyle}>
            <ToolbarDropdown
              id={'graph_type_dropdown'}
              disabled={props.disabled || props.isNodeGraph}
              handleSelect={setGraphType}
              value={graphTypeKey}
              label={GRAPH_TYPES[graphTypeKey]}
              options={GRAPH_TYPES}
            />
          </span>
        </TourStop>

        <div className={rightToolbarStyle}>
          <TourStop info={GraphTourStops.TimeRange}>
            <TimeDurationComponent id="graph_time_range" disabled={props.disabled} supportsReplay={true} />
          </TourStop>
        </div>
      </>
    </SecondaryMasthead>
  );
};
