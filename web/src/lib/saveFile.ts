/** Hands bytes to the browser as a download. Components take it as a prop so tests can replace it. */
export type SaveFile = (name: string, bytes: Uint8Array) => void;

export const browserSaveFile: SaveFile = (name, bytes) => {
  const url = URL.createObjectURL(new Blob([bytes.slice().buffer]));
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
};
