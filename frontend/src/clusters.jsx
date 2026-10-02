import { createContext, useContext } from "react";
import { useApi } from "./api.js";

// The list of clusters is needed in many places (sidebar, top bar, pages),
// so we load it once and share it through React context instead of passing
// it down through every component.
const ClustersContext = createContext(null);

export function ClustersProvider({ children }) {
  const clusters = useApi("/clusters");
  return <ClustersContext.Provider value={clusters}>{children}</ClustersContext.Provider>;
}

// useClusters returns { data: [...clusters], error, loading, reload }.
export function useClusters() {
  return useContext(ClustersContext);
}
