declare module "monaco-themes/themes/*.json" {
  import type * as Monaco from "monaco-editor";
  const themeData: Monaco.editor.IStandaloneThemeData;
  export default themeData;
}
