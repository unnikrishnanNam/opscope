import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { api } from "../api.js";
import { useClusters } from "../clusters.jsx";
import ErrorBox from "../components/ErrorBox.jsx";

// Steps: paste or upload a kubeconfig → we list its contexts → pick one,
// name it → the backend tests the connection and saves it.
export default function AddCluster() {
  const navigate = useNavigate();
  const { reload } = useClusters();

  const [name, setName] = useState("");
  const [kubeconfig, setKubeconfig] = useState("");
  const [contexts, setContexts] = useState([]);
  const [context, setContext] = useState("");
  const [readError, setReadError] = useState(null);
  const [saveError, setSaveError] = useState(null);
  const [saving, setSaving] = useState(false);

  // Each time the kubeconfig text changes, ask the backend which contexts it
  // has. We wait 400 ms after the last keystroke so we don't call on every key.
  useEffect(() => {
    if (!kubeconfig.trim()) {
      setContexts([]);
      setReadError(null);
      return;
    }
    const timer = setTimeout(async () => {
      try {
        const result = await api("/clusters/inspect", { method: "POST", body: { kubeconfig } });
        setContexts(result.contexts);
        setContext(result.current || result.contexts[0]?.name || "");
        setReadError(null);
      } catch (err) {
        setContexts([]);
        setReadError(err);
      }
    }, 400);
    return () => clearTimeout(timer);
  }, [kubeconfig]);

  async function loadFile(event) {
    const file = event.target.files[0];
    if (file) setKubeconfig(await file.text());
  }

  async function save(event) {
    event.preventDefault(); // stop the browser from submitting the form itself
    setSaving(true);
    setSaveError(null);
    try {
      const cluster = await api("/clusters", { method: "POST", body: { name, kubeconfig, context } });
      reload();
      navigate(`/c/${cluster.id}/overview`);
    } catch (err) {
      setSaveError(err);
      setSaving(false);
    }
  }

  const chosen = contexts.find((c) => c.name === context);

  return (
    <section className="narrow">
      <h1 className="page-title">Add a cluster</h1>
      <p className="page-about">
        Paste a kubeconfig or load it from a file. Opscope checks that it can connect, then saves only the context
        you pick.
      </p>

      <form onSubmit={save} className="form">
        <div className="field">
          <div className="field-row">
            <label htmlFor="kubeconfig" className="field-label">
              Kubeconfig
            </label>
            <label className="button button-quiet">
              Load from file
              <input type="file" onChange={loadFile} hidden />
            </label>
          </div>
          <textarea
            id="kubeconfig"
            className="input textarea"
            rows={12}
            spellCheck={false}
            placeholder={"apiVersion: v1\nkind: Config\nclusters:\n  ..."}
            value={kubeconfig}
            onChange={(e) => setKubeconfig(e.target.value)}
          />
          {readError && <div className="field-error">{readError.message}</div>}
          <div className="field-hint">
            Credentials must be embedded in the file. Kubeconfigs that log in with a command (for example cloud CLI
            plugins) can only be used through <code>OPSCOPE_KUBECONFIG</code>. Tip:{" "}
            <code>kubectl config view --minify --flatten</code> prints an embedded copy of your current context.
          </div>
        </div>

        {contexts.length > 0 && (
          <div className="field">
            <label htmlFor="context" className="field-label">
              Context
            </label>
            <select id="context" className="select" value={context} onChange={(e) => setContext(e.target.value)}>
              {contexts.map((c) => (
                <option key={c.name} value={c.name}>
                  {c.name}
                </option>
              ))}
            </select>
            {chosen && <div className="field-hint mono">{chosen.server}</div>}
          </div>
        )}

        <div className="field">
          <label htmlFor="name" className="field-label">
            Display name
          </label>
          <input
            id="name"
            className="input"
            placeholder="e.g. Home lab"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>

        {saveError && <ErrorBox title="Couldn't add the cluster" message={saveError.message} detail={saveError.detail} />}

        <div>
          <button type="submit" className="button button-primary" disabled={saving || !context || !name.trim()}>
            {saving ? "Connecting…" : "Test connection and save"}
          </button>
        </div>
      </form>
    </section>
  );
}
