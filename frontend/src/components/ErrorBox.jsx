// A plain, readable error message. `detail` (the raw error) is tucked away
// behind "Technical details" so it's there when needed but doesn't shout.
export default function ErrorBox({ title, message, detail }) {
  return (
    <div className="error-box" role="alert">
      {title && <div className="error-title">{title}</div>}
      <div>{message}</div>
      {detail && (
        <details>
          <summary>Technical details</summary>
          <code>{detail}</code>
        </details>
      )}
    </div>
  );
}
