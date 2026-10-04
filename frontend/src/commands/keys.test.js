import { describe, expect, test } from "vitest";
import { matchShortcut } from "./keys.js";

const pods = { id: "go.pods", shortcut: "g p" };
const nodes = { id: "go.nodes", shortcut: "g n" };
const filter = { id: "table.filter", shortcut: "/" };
const help = { id: "help", shortcut: "?" };
const commands = [pods, nodes, filter, help];

describe("matchShortcut", () => {
  test("a single key runs its command", () => {
    expect(matchShortcut(commands, [], "/")).toEqual({ command: filter });
    expect(matchShortcut(commands, [], "?")).toEqual({ command: help });
  });

  test("a sequence waits for its next key", () => {
    expect(matchShortcut(commands, [], "g")).toEqual({ pending: ["g"] });
    expect(matchShortcut(commands, ["g"], "p")).toEqual({ command: pods });
    expect(matchShortcut(commands, ["g"], "n")).toEqual({ command: nodes });
  });

  test("an unknown key does nothing", () => {
    expect(matchShortcut(commands, [], "x")).toBeNull();
    expect(matchShortcut(commands, ["g"], "x")).toBeNull();
  });

  test("a broken sequence still lets a single-key shortcut through", () => {
    expect(matchShortcut(commands, ["g"], "/")).toEqual({ command: filter });
  });

  test("keys are compared exactly (P isn't p)", () => {
    expect(matchShortcut(commands, ["g"], "P")).toBeNull();
  });

  test("the first command with a shortcut wins", () => {
    const pageFilter = { id: "page.filter", shortcut: "/" };
    expect(matchShortcut([pageFilter, ...commands], [], "/")).toEqual({ command: pageFilter });
  });
});
