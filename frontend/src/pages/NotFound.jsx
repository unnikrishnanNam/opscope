import { Link } from "react-router";

export default function NotFound() {
  return (
    <section>
      <h1 className="page-title">Page not found</h1>
      <p className="page-about">
        There is nothing at this address. <Link to="/">Go to the start</Link>.
      </p>
    </section>
  );
}
