import { lazy, Suspense } from "react";
import { Navigate, Route, Routes } from "react-router";
import Layout from "./components/Layout.jsx";
import Home from "./pages/Home.jsx";
import ManageClusters from "./pages/ManageClusters.jsx";
import AddCluster from "./pages/AddCluster.jsx";
import Placeholder from "./pages/Placeholder.jsx";
import ResourceList from "./pages/ResourceList.jsx";
import Overview from "./pages/Overview.jsx";
import ResourceDetail from "./pages/ResourceDetail.jsx";
import NotFound from "./pages/NotFound.jsx";
import { allPages } from "./sections.js";

// The component kit (/kit) exists only in development. In a production build
// import.meta.env.DEV is false, so this whole branch and the kit's code are
// left out of the bundle.
const Kit = import.meta.env.DEV ? lazy(() => import("./kit/Kit.jsx")) : null;

// All routes. Every page renders inside <Layout> (sidebar + top bar).
//
//   /                         picks a cluster, or sends you to "Add a cluster"
//   /clusters                 list and remove clusters
//   /clusters/add             add a cluster
//   /c/:clusterId/<page>      a page for one cluster, e.g. /c/lab/workloads/pods
//   /c/:clusterId/<page>/:namespace/:name   one object, e.g. /c/lab/workloads/pods/web/api-1
//   /c/:clusterId/<page>/:name              one cluster-wide object, e.g. /c/lab/nodes/worker-1
//   ...?ns=default            the selected namespace (none = all namespaces)
//   /kit                      component kit, development only (outside the layout)
export default function App() {
  return (
    <Routes>
      {Kit && (
        <Route
          path="kit"
          element={
            <Suspense>
              <Kit />
            </Suspense>
          }
        />
      )}
      <Route element={<Layout />}>
        <Route index element={<Home />} />
        <Route path="clusters" element={<ManageClusters />} />
        <Route path="clusters/add" element={<AddCluster />} />
        <Route path="c/:clusterId">
          <Route index element={<Navigate to="overview" replace />} />
          {allPages.map((page) => (
            <Route key={page.path} path={page.path} element={pageElement(page)} />
          ))}
          {allPages
            .filter((page) => page.resource)
            .map((page) => (
              <Route
                key={`${page.path}/detail`}
                path={page.clusterScoped ? `${page.path}/:name` : `${page.path}/:namespace/:name`}
                element={<ResourceDetail page={page} />}
              />
            ))}
        </Route>
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  );
}

// The overview has its own page; pages with a `resource` are tables; the
// rest are placeholders until their phase arrives.
function pageElement(page) {
  if (page.path === "overview") return <Overview page={page} />;
  return page.resource ? <ResourceList page={page} /> : <Placeholder page={page} />;
}
