export const getStorageItem = key => JSON.parse(window.localStorage.getItem('GLOUTON_STORAGE_' + key))

export const setStorageItem = (key, newValue) => {
  window.localStorage.setItem('GLOUTON_STORAGE_' + key, JSON.stringify(newValue))
}
