import React from 'react';
import { kialiStyle } from 'styles/StyleUtils';
import { RenderComponentScroll } from './RenderComponentScroll';

const containerPadding = kialiStyle({ padding: '30px 20px 0 20px' });

export class RenderContent extends React.Component<{ needScroll?: boolean }> {
  render() {
    return (
      <RenderComponentScroll className={containerPadding}>
        <div>{this.props.children}</div>
      </RenderComponentScroll>
    );
  }
}
