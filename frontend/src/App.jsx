import { Navigate, Route, Routes } from "react-router";
import Sidebar from "./components/Sidebar.jsx";
import TopBar from "./components/TopBar.jsx";
import Placeholder from "./pages/Placeholder.jsx";
import NotFound from "./pages/NotFound.jsx";
import { allPages } from "./sections.js";

// The app shell: sidebar on the left, top bar and page content on the right.
export default function App() {
  return (
    <div className="shell">
      <Sidebar />
      <div className="main">
        <TopBar />
        <main className="content">
          <Routes>
            <Route path="/" element={<Navigate to="/overview" replace />} />
            {allPages.map((page) => (
              <Route key={page.path} path={page.path} element={<Placeholder page={page} />} />
            ))}
            <Route path="*" element={<NotFound />} />
          </Routes>
        </main>
      </div>
    </div>
  );
}
