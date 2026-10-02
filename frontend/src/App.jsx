import { Navigate, Route, Routes } from "react-router";
import Layout from "./components/Layout.jsx";
import Home from "./pages/Home.jsx";
import ManageClusters from "./pages/ManageClusters.jsx";
import AddCluster from "./pages/AddCluster.jsx";
import Placeholder from "./pages/Placeholder.jsx";
import ResourceList from "./pages/ResourceList.jsx";
import NotFound from "./pages/NotFound.jsx";
import { allPages } from "./sections.js";

// All routes. Every page renders inside <Layout> (sidebar + top bar).
//
//   /                         picks a cluster, or sends you to "Add a cluster"
//   /clusters                 list and remove clusters
//   /clusters/add             add a cluster
//   /c/:clusterId/<page>      a page for one cluster, e.g. /c/lab/workloads/pods
//   ...?ns=default            the selected namespace (none = all namespaces)
export default function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Home />} />
        <Route path="clusters" element={<ManageClusters />} />
        <Route path="clusters/add" element={<AddCluster />} />
        <Route path="c/:clusterId">
          <Route index element={<Navigate to="overview" replace />} />
          {allPages.map((page) => (
            <Route key={page.path} path={page.path} element={pageElement(page)} />
          ))}
        </Route>
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  );
}

// Pages with a `resource` are tables built in this phase or earlier; the
// rest are placeholders until their phase arrives.
function pageElement(page) {
  return page.resource ? <ResourceList page={page} /> : <Placeholder page={page} />;
}
