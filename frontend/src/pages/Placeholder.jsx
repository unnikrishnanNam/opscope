// Stand-in for pages that are not built yet. Each one is replaced by the
// real page in the phase listed in docs/PHASES.md.
export default function Placeholder({ page }) {
  return (
    <section>
      <h1 className="page-title">{page.label}</h1>
      <p className="page-about">{page.about}</p>

      <div className="notice">
        <span className="notice-label">Phase {page.phase}</span>
        This page is planned but not built yet.
      </div>
    </section>
  );
}
