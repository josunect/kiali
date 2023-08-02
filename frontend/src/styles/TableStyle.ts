import { kialiStyle } from './StyleUtils';

export const tableStyle = kialiStyle({
  width: '100%',
  maxWidth: '100%',
  $nest: {
    // eslint-disable-next-line no-multi-str
    '& > tbody > tr > td, \
     & > tbody > tr > th, \
     & > tfoot > tr > td, \
     & > tfoot > tr > th, \
     & > thead > tr > td, \
     & > thead > tr > th': {
      padding: '10px',
      lineHeight: 1.66667,
      verticalAlign: 'top',
      borderTop: '1px solid #d1d1d1'
    },

    '& > thead > tr > th': {
      verticalAlign: 'bottom',
      borderBottom: '2px solid #d1d1d1'
    },

    // eslint-disable-next-line no-multi-str
    '& > thead:first-child > tr:first-child > td, \
     & > thead:first-child > tr:first-child > th': {
      borderTop: 0
    }
  }
});
