import { KIALI_THEME, Theme } from 'types/Common';

export const getTheme = (): Theme => {
  return (localStorage.getItem(KIALI_THEME) as Theme) ?? getDefaultTheme();
};

// Get default theme from system settings
const getDefaultTheme = (): Theme => {
  if (window.matchMedia('(prefers-color-scheme: dark)').matches) {
    return Theme.DARK;
  }

  return Theme.LIGHT;
};
