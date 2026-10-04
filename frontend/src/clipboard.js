import { toast } from "./toast.js";

// copyText puts text on the clipboard and says so in a toast:
//   copyText(name, "the name")  ->  "Copied the name"
// The browser can refuse (no permission, an insecure page); the toast then
// says that instead. Returns whether it worked.
export async function copyText(text, what) {
  try {
    await navigator.clipboard.writeText(text);
    toast(`Copied ${what}`);
    return true;
  } catch {
    toast(`Couldn't copy ${what}: the browser didn't allow it`, { tone: "error" });
    return false;
  }
}
