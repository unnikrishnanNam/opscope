import { describe, expect, test } from "vitest";
import { matchCommand, matchWord, queryWords, segments } from "./match.js";

describe("queryWords", () => {
  test("splits on spaces and lowers the case", () => {
    expect(queryWords("  Po   API ")).toEqual(["po", "api"]);
    expect(queryWords("   ")).toEqual([]);
  });
});

describe("matchWord", () => {
  test("each kind of match beats the next", () => {
    const exact = matchWord("pods", "Pods").score;
    const prefix = matchWord("dep", "Deployments").score;
    const wordStart = matchWord("api", "checkout-api-6b7c").score;
    const inside = matchWord("ploy", "Deployments").score;
    const letters = matchWord("dply", "Deployments").score;
    expect(exact).toBeGreaterThan(prefix);
    expect(prefix).toBeGreaterThan(wordStart);
    expect(wordStart).toBeGreaterThan(inside);
    expect(inside).toBeGreaterThan(letters);
    expect(letters).toBeGreaterThan(0);
  });

  test("returns the matched positions", () => {
    expect(matchWord("pods", "Pods").positions).toEqual([0, 1, 2, 3]);
    expect(matchWord("api", "checkout-api-6b7c").positions).toEqual([9, 10, 11]);
    expect(matchWord("dply", "Deployments").positions).toEqual([0, 2, 3, 5]);
  });

  test("a closer fit ranks higher within the same kind", () => {
    expect(matchWord("pod", "Pods").score).toBeGreaterThan(matchWord("pod", "PodDisruptionBudgets").score);
  });

  test("finds word starts after punctuation, capitals and digits", () => {
    expect(matchWord("sets", "StatefulSets").positions).toEqual([8, 9, 10, 11]);
    expect(matchWord("sets", "StatefulSets").score).toBeGreaterThanOrEqual(60);
    expect(matchWord("2", "worker2").score).toBeGreaterThanOrEqual(60);
    expect(matchWord("1", "api-1").positions).toEqual([4]);
  });

  test("prefers a word start over an earlier match inside a word", () => {
    // "ap" is inside "snapshot" before it starts "api".
    expect(matchWord("ap", "snapshot-api").positions).toEqual([9, 10]);
  });

  test("letters have to start at the start of a word", () => {
    expect(matchWord("sts", "StatefulSets")).not.toBeNull();
    expect(matchWord("ply", "Deployments")).toBeNull();
  });

  test("letters keep the best start", () => {
    // From "web" the letters spread out; from "worker" they sit together.
    expect(matchWord("wrk", "web-worker").positions).toEqual([4, 6, 7]);
  });

  test("no match", () => {
    expect(matchWord("zzz", "Deployments")).toBeNull();
    expect(matchWord("podss", "Pods")).toBeNull();
  });
});

describe("matchCommand", () => {
  const services = { id: "go.services", title: "Services", keywords: ["svc", "service"], detail: "Network" };
  const pod = { id: "pod.api", title: "api-1", detail: "web", keywords: ["pods", "pod", "po"] };

  test("every word has to match", () => {
    expect(matchCommand(["po", "api"], pod)).not.toBeNull();
    expect(matchCommand(["po", "zzz"], pod)).toBeNull();
  });

  test("words can match the title, keywords or detail", () => {
    expect(matchCommand(["svc"], services)).not.toBeNull();
    expect(matchCommand(["network"], services)).not.toBeNull();
  });

  test("the same match counts for less outside the title", () => {
    const inTitle = matchCommand(["web"], { id: "a", title: "web" }).score;
    const inKeyword = matchCommand(["web"], { id: "b", title: "x", keywords: ["web"] }).score;
    const inDetail = matchCommand(["web"], { id: "c", title: "x", detail: "web" }).score;
    expect(inTitle).toBeGreaterThan(inKeyword);
    expect(inKeyword).toBeGreaterThan(inDetail);
  });

  test("returns positions to make bold in the title and detail, not keywords", () => {
    expect(matchCommand(["api", "web"], pod)).toMatchObject({ title: [0, 1, 2], detail: [0, 1, 2] });
    expect(matchCommand(["svc"], services)).toMatchObject({ title: [], detail: [] });
  });

  test("two words in the same text both show", () => {
    expect(matchCommand(["api", "1"], pod).title).toEqual([0, 1, 2, 4]);
  });
});

describe("segments", () => {
  test("splits text into matched and plain runs", () => {
    expect(segments("api-1", [0, 1, 4])).toEqual([
      { text: "ap", match: true },
      { text: "i-", match: false },
      { text: "1", match: true },
    ]);
  });

  test("no positions is one plain run", () => {
    expect(segments("Pods", [])).toEqual([{ text: "Pods", match: false }]);
  });
});
