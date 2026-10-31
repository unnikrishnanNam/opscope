// Volume usage: how full each claim's volume is. The backend reads it from
// the kubelets when OPSCOPE_VOLUME_USAGE is on (GET .../metrics/volumes);
// these helpers turn its answer into what the pages show.

import { percent, size } from "./format.js";

// Error codes that mean "usage is off" or "not allowed", which
// VolumeUsageNote explains.
export const VOLUME_USAGE_NOTES = ["volume_usage_off", "volume_usage_forbidden"];

// For useApi's stopOn: off is a server setting, so asking again until
// Opscope restarts would only get the same answer.
export const USAGE_OFF = ["volume_usage_off"];

// At this fullness (space or inodes), a claim counts as nearly full. Same
// threshold as the red usage bar.
export const NEARLY_FULL = 90;

// usageByClaim maps "namespace/name" to the claim's usage; to
// { unread: { node, error } } when its node couldn't be read; or to
// { unmeasured: true } when it's mounted but the kubelet gave no numbers.
// Claims that are in none of these aren't mounted by a running pod.
export function usageByClaim(report) {
  const byKey = {};
  for (const claim of report?.unmeasured ?? []) byKey[claim] = { unmeasured: true };
  for (const node of report?.unread ?? []) for (const claim of node.claims) byKey[claim] = { unread: node };
  // A claim mounted on two nodes, one unread, still has its numbers.
  for (const v of report?.volumes ?? []) byKey[`${v.namespace}/${v.name}`] = v;
  return byKey;
}

// Why a mounted claim has no numbers, for hover text and notes.
export const UNMEASURED_TEXT =
  "The kubelet reports no usage for this volume. It doesn't measure volumes that are folders on the node's disk " +
  "(hostPath, which local-path uses), and a volume mounted moments ago has no numbers yet.";

// How full a volume is, in percent. Full means no space left for the pod,
// so it's measured from what's available, not from used: filesystems keep
// some space back, and used + available is often less than the capacity.
export function spaceFull(u) {
  return percent(u.capacity - u.available, u.capacity);
}

export function inodesFull(u) {
  return percent(u.inodesUsed, u.inodes);
}

export function isNearlyFull(u) {
  return spaceFull(u) >= NEARLY_FULL || inodesFull(u) >= NEARLY_FULL;
}

// "2.1 GiB used, 7.7 GiB free of 9.8 GiB"
export function spaceText(u) {
  return `${size(u.used)} used, ${size(u.available)} free of ${size(u.capacity)}`;
}

// "1,204 of 655,360 inodes (1%)"
export function inodesText(u) {
  return `${u.inodesUsed.toLocaleString()} of ${u.inodes.toLocaleString()} inodes (${inodesFull(u)}%)`;
}
