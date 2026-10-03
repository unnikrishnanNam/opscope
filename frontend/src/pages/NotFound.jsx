import Button from "../components/Button.jsx";
import { EmptyState } from "../components/Callout.jsx";
import { SearchIcon } from "../components/icons.jsx";

export default function NotFound() {
  return (
    <EmptyState
      icon={SearchIcon}
      title="Page not found"
      action={
        <Button to="/" variant="primary">
          Go to the start
        </Button>
      }
    >
      There's nothing at this address.
    </EmptyState>
  );
}
