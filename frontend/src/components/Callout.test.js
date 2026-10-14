import { describe, expect, test } from "vitest";
import { messageParts } from "./Callout.jsx";

describe("messageParts", () => {
  test("backticks mark code", () => {
    expect(messageParts("Put it on kind's network (`--network kind`) and try again.")).toEqual([
      { text: "Put it on kind's network (", code: false },
      { text: "--network kind", code: true },
      { text: ") and try again.", code: false },
    ]);
  });

  test("code at the start and end", () => {
    expect(messageParts("`a` and `b`")).toEqual([
      { text: "a", code: true },
      { text: " and ", code: false },
      { text: "b", code: true },
    ]);
  });

  test("a message without backticks is one part", () => {
    expect(messageParts("Couldn't reach the API server.")).toEqual([{ text: "Couldn't reach the API server.", code: false }]);
  });

  test("an unpaired backtick is left as it is", () => {
    expect(messageParts("it's a `typo")).toEqual([{ text: "it's a `typo" }]);
  });
});
