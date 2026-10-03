import { useState } from "react";
import { api } from "../api.js";
import Button from "./Button.jsx";
import { Callout } from "./Callout.jsx";
import CodeBlock from "./CodeBlock.jsx";
import { Tag } from "./Tag.jsx";
import { EyeIcon, EyeOffIcon } from "./icons.jsx";
import "./SecretKeys.css";

// The keys of one secret. Values stay hidden until "Reveal" is clicked for
// that one key; only then is the value fetched from the backend. Nothing is
// saved: hiding the value or leaving the page forgets it.
export default function SecretKeys({ cluster, secret }) {
  if (secret.keys.length === 0) return <p className="secret-keys-note">This secret has no keys.</p>;

  return (
    <div className="secret-keys">
      <p className="secret-keys-note">
        Values are fetched one at a time when you reveal them, and aren't stored by Opscope.
      </p>
      <ul className="secret-key-list">
        {secret.keys.map((key) => (
          <SecretKey key={key} cluster={cluster} secret={secret} name={key} />
        ))}
      </ul>
    </div>
  );
}

function SecretKey({ cluster, secret, name }) {
  const [value, setValue] = useState(null); // { value, base64 } once revealed
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(false);

  async function reveal() {
    setLoading(true);
    setError(null);
    try {
      // Every part goes through encodeURIComponent so odd names can't break the URL.
      const path = [cluster.id, "secrets", secret.namespace, secret.name, name].map(encodeURIComponent).join("/");
      setValue(await api(`/clusters/${path}`));
    } catch (err) {
      setError(err);
    }
    setLoading(false);
  }

  return (
    <li className="secret-key">
      <div className="secret-key-row">
        <span className="secret-key-name">{name}</span>
        {value ? (
          <>
            {value.base64 && <Tag>binary, shown as base64</Tag>}
            <Button variant="quiet" size="sm" icon={EyeOffIcon} onClick={() => setValue(null)}>
              Hide
            </Button>
          </>
        ) : (
          <>
            <span className="secret-key-mask" aria-label="hidden value">
              ••••••••••••
            </span>
            <Button variant="quiet" size="sm" icon={EyeIcon} onClick={reveal} loading={loading}>
              Reveal
            </Button>
          </>
        )}
      </div>
      {value && (
        <div className="secret-key-value">
          <CodeBlock code={value.value} lineNumbers={false} />
        </div>
      )}
      {error && (
        <div className="secret-key-value">
          <Callout compact tone="error">
            {error.message}
          </Callout>
        </div>
      )}
    </li>
  );
}
