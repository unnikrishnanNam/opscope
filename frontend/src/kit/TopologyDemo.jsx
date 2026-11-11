import { useMemo } from "react";
import TopologyView from "../components/TopologyView.jsx";
import { gapsAnswer, largeAnswer, shopAnswer } from "./topologySamples.js";

// The topology map on /kit, on sample graphs, before any page uses it.

function Shop() {
  const answer = useMemo(() => shopAnswer(), []);
  return <TopologyView answer={answer} clusterId="kit" label="Map of shop" height={560} fitKey="shop" />;
}

function AroundOneObject() {
  const answer = useMemo(() => shopAnswer({ focus: "deployments/shop/api" }), []);
  return (
    <TopologyView
      answer={answer}
      clusterId="kit"
      label="Map around Deployment api"
      height={520}
      fitKey="focus"
      initial={{ selected: answer.focus }}
    />
  );
}

function Gaps() {
  const answer = useMemo(() => gapsAnswer(), []);
  return (
    <TopologyView
      answer={answer}
      clusterId="kit"
      label="Map of team-a"
      height={440}
      fitKey="gaps"
      initial={{ selected: answer.nodes.find((n) => n.presence === "missing").id }}
    />
  );
}

function Large() {
  const answer = useMemo(() => largeAnswer(), []);
  return <TopologyView answer={answer} clusterId="kit" label="Map of big" height={560} fitKey="large" />;
}

function AsList() {
  const answer = useMemo(() => shopAnswer(), []);
  return <TopologyView answer={answer} clusterId="kit" label="Map of shop" initial={{ mode: "list" }} />;
}

function Empty() {
  return (
    <TopologyView
      answer={{ built: "live", partial: [], nodes: [], edges: [] }}
      clusterId="kit"
      label="Map of an empty namespace"
      height={200}
      fitKey="empty"
      empty="Nothing in this namespace yet."
    />
  );
}

export const topologySections = [
  {
    id: "topology-map",
    title: "Topology map",
    about:
      "A namespace with every kind of edge. Columns run the way traffic does, then what pods need. Click a card for its details; drag to pan, ⌘/Ctrl-scroll or pinch to zoom. Tab into the map, then arrows move along edges.",
    render: () => <Shop />,
  },
  {
    id: "topology-focus",
    title: "Map around one object",
    about: "The object the map is about has the orange line; its panel is open, and what isn't connected to it steps back.",
    render: () => <AroundOneObject />,
  },
  {
    id: "topology-gaps",
    title: "Gaps",
    about:
      "Secrets can't be read (as without secrets-access.yaml), an owner of a kind Opscope doesn't know, a Service whose selector matches nothing, and a ConfigMap referred to that doesn't exist.",
    render: () => <Gaps />,
  },
  {
    id: "topology-large",
    title: "A large namespace",
    about:
      "About 1,000 objects: 110 apps of three pods, with their ReplicaSets and EndpointSlices. Each app's pods are one card and the rest are folded in, so the map has 560 cards.",
    render: () => <Large />,
  },
  {
    id: "topology-list",
    title: "Map as a list",
    about: "The same objects and connections as text, in the same columns: what screen readers and phones get.",
    render: () => <AsList />,
  },
  {
    id: "topology-empty",
    title: "Empty map",
    about: "A namespace with nothing in it.",
    render: () => <Empty />,
  },
];
