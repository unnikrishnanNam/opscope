import { ErrorIcon, WarningIcon } from "./icons.jsx";
import "./StatusBadge.css";

// One place that decides the colour of every status word, so "Failed"
// looks the same on pods, jobs and everything else.
//
//   ok      healthy or finished well
//   warn    on its way, or waiting on something
//   bad     broken and probably needs a person
//   neutral nothing to worry about either way

const OK = ["Running", "Succeeded", "Completed", "Complete", "Active", "Ready", "Programmed", "Accepted"];
const WARN = ["Pending", "ContainerCreating", "PodInitializing", "Terminating", "Suspended", "NotReady"];
const BAD = [
  "Failed",
  "Error",
  "CrashLoopBackOff",
  "ImagePullBackOff",
  "ErrImagePull",
  "InvalidImageName",
  "CreateContainerConfigError",
  "CreateContainerError",
  "OOMKilled",
  "Evicted",
  "Unknown",
  "NotProgrammed",
  "NotAccepted",
  "UnresolvedRefs",
];

export function statusTone(status) {
  if (OK.includes(status)) return "ok";
  if (BAD.includes(status)) return "bad";
  if (WARN.includes(status)) return "warn";
  // "Init:1/2" is progress; "Init:CrashLoopBackOff" or "Init:Error" is a problem.
  if (status?.startsWith("Init:")) return BAD.includes(status.slice(5)) ? "bad" : "warn";
  if (status?.startsWith("ExitCode:")) return "bad";
  return "neutral";
}

// StatusBadge: a status word with a mark in front. Healthy and neutral get a
// dot; warn and bad get an icon, so a problem stands out even without colour.
// `title` is shown on hover, e.g. the reason a gateway isn't programmed.
export default function StatusBadge({ status, title }) {
  const tone = statusTone(status);
  const Icon = tone === "bad" ? ErrorIcon : tone === "warn" ? WarningIcon : null;
  return (
    <span className={`status status-${tone} ${Icon ? "status-has-icon" : ""}`} title={title || undefined}>
      {Icon && <Icon size={14} className="status-icon" />}
      {status || "–"}
    </span>
  );
}

// "2/3" for ready-style counts, highlighted when fewer than wanted are ready.
export function Fraction({ have, want }) {
  return <span className={have < want ? "fraction-short" : undefined}>{`${have}/${want}`}</span>;
}
