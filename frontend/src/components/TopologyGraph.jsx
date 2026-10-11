import { useEffect, useId, useImperativeHandle, useLayoutEffect, useMemo, useRef, useState } from "react";
import { HEADER_H, NODE_H, NODE_W, layout } from "../graph/layout.js";
import { STATE_WORDS, kindIcon } from "../graph/kinds.js";
import { causeChain, chainEdges, indexFindings, rootCauses, shownState } from "../graph/trace.js";
import Button from "./Button.jsx";
import { CrosshairIcon, ErrorIcon, FitIcon, MinusIcon, PlusIcon, WarningIcon } from "./icons.jsx";
import "./TopologyGraph.css";

const MIN_ZOOM = 0.2;
const MAX_ZOOM = 2;
const READABLE = 0.8; // below this, the first view shows the focus rather than everything
const PAD = 24;
const DRAG = 4; // px a press must move before it pans instead of clicking

// TopologyGraph draws a view of the graph: cards in columns by kind, the
// edges between them, and the chain from the selected card (or the focused
// one) down to its root causes, in the colour of the problem.
//
//   view       the topology endpoint's answer: { nodes, edges, findings }
//   focus      the object the map is about ("you are here"), if any
//   namespace  the namespace being looked at; objects elsewhere show theirs
//   selected   the card whose details are open; onSelect(id or null)
//   label      names the map for screen readers
//   inset      px on the right covered by a panel, kept clear when showing a card
//   fitKey     the view is fitted on screen again when this changes
//   ref        { showCard(id), focusCard(id), fit() }
//
// Drag or scroll to pan; zoom with ⌘/Ctrl and the wheel, a pinch, the
// buttons or + and −. Arrow keys move along edges, Enter opens a card.
export default function TopologyGraph({ view, focus, namespace, selected, onSelect, label, inset = 0, fitKey, ref }) {
  const hintId = useId();
  const viewportRef = useRef(null);
  const cards = useRef(new Map());

  // Cards keep their order from one refresh to the next.
  const previous = useRef(null);
  const placed = useMemo(() => layout(view, { previous: previous.current?.order }), [view]);
  useEffect(() => {
    previous.current = placed;
  }, [placed]);

  const index = useMemo(() => indexFindings(view), [view]);
  const roots = useMemo(() => rootCauses(index), [index]);
  const traced = selected ?? focus;
  const chain = useMemo(() => causeChain(index, traced), [index, traced]);
  const lit = useMemo(() => chainEdges(chain, placed.edges, placed.nodes), [chain, placed]);

  // ---- Pan and zoom ----

  const [size, setSize] = useState(null);
  const [t, setT] = useState(null); // { x, y, k }: content point p is drawn at p·k + (x, y)
  const fitted = useRef(undefined);

  useLayoutEffect(() => {
    const el = viewportRef.current;
    const observer = new ResizeObserver(([entry]) => {
      setSize({ w: entry.contentRect.width, h: entry.contentRect.height });
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  useLayoutEffect(() => {
    if (!size || (t && fitted.current === fitKey)) return;
    fitted.current = fitKey;
    setT(firstView(placed, size, focus));
  }, [size, fitKey, placed, focus, t]);

  function zoomAt(factor, cx, cy) {
    setT((t) => {
      const k = clamp(t.k * factor, MIN_ZOOM, MAX_ZOOM);
      return { k, x: cx - ((cx - t.x) * k) / t.k, y: cy - ((cy - t.y) * k) / t.k };
    });
  }
  // The width not covered by a panel; on a narrow screen the panel covers
  // the map, so all of it.
  const free = size ? size.w - (inset < size.w * 0.6 ? inset : 0) : 0;
  const zoomCentre = (factor) => size && zoomAt(factor, free / 2, size.h / 2);
  const fit = () => size && setT(fitAll(placed, { ...size, w: free }));

  // Keeps a card on screen, moving as little as possible.
  function showCard(id) {
    const n = placed.nodes[id];
    if (!n || !size) return;
    setT((t) => {
      const left = n.x * t.k + t.x;
      const top = n.y * t.k + t.y;
      const right = left + n.w * t.k;
      const bottom = top + n.h * t.k;
      let dx = 0;
      let dy = 0;
      if (left < PAD) dx = PAD - left;
      else if (right > free - PAD) dx = Math.max(free - PAD - right, PAD - left);
      if (top < PAD) dy = PAD - top;
      else if (bottom > size.h - PAD) dy = Math.max(size.h - PAD - bottom, PAD - top);
      return dx || dy ? { ...t, x: t.x + dx, y: t.y + dy } : t;
    });
  }

  // Puts a card in the middle of what's free of the panel.
  function centreCard(id) {
    const n = placed.nodes[id];
    if (!n || !size) return;
    setT((t) => ({ ...t, x: free / 2 - (n.x + n.w / 2) * t.k, y: size.h / 2 - (n.y + n.h / 2) * t.k }));
  }

  function focusCard(id) {
    setActive(id);
    cards.current.get(id)?.focus({ preventScroll: true });
    showCard(id);
  }

  useImperativeHandle(ref, () => ({ showCard, focusCard, fit }));

  // The wheel pans, or zooms with ⌘/Ctrl (a trackpad pinch sends that too).
  // It's attached by hand: React's wheel listener can't stop the page scrolling.
  useEffect(() => {
    const el = viewportRef.current;
    function onWheel(event) {
      event.preventDefault();
      const unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? el.clientHeight : 1;
      const rect = el.getBoundingClientRect();
      if (event.ctrlKey || event.metaKey) {
        zoomAt(Math.exp(-event.deltaY * unit * 0.01), event.clientX - rect.left, event.clientY - rect.top);
      } else {
        setT((t) => ({ ...t, x: t.x - event.deltaX * unit, y: t.y - event.deltaY * unit }));
      }
    }
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []);

  // Dragging pans, from the background or a card; two fingers pinch.
  const pointers = useRef(new Map());
  const gesture = useRef(null);
  const [panning, setPanning] = useState(false);

  function onPointerDown(event) {
    if (event.button !== 0 || event.target.closest(".topo-controls")) return;
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY });
    if (pointers.current.size === 1) gesture.current = { startX: event.clientX, startY: event.clientY, moved: false };
  }

  function onPointerMove(event) {
    const last = pointers.current.get(event.pointerId);
    const g = gesture.current;
    if (!last || !g) return;
    const now = { x: event.clientX, y: event.clientY };
    if (pointers.current.size === 2) {
      const other = [...pointers.current].find(([id]) => id !== event.pointerId)[1];
      const rect = viewportRef.current.getBoundingClientRect();
      const before = Math.hypot(last.x - other.x, last.y - other.y);
      const after = Math.hypot(now.x - other.x, now.y - other.y);
      if (before > 0) zoomAt(after / before, (now.x + other.x) / 2 - rect.left, (now.y + other.y) / 2 - rect.top);
      pointers.current.set(event.pointerId, now);
      g.moved = true;
      return;
    }
    if (!g.moved) {
      if (Math.hypot(now.x - g.startX, now.y - g.startY) < DRAG) return;
      g.moved = true;
      viewportRef.current.setPointerCapture(event.pointerId);
      setPanning(true);
    }
    pointers.current.set(event.pointerId, now);
    setT((t) => ({ ...t, x: t.x + now.x - last.x, y: t.y + now.y - last.y }));
  }

  function onPointerUp(event) {
    pointers.current.delete(event.pointerId);
    if (pointers.current.size === 0) setPanning(false);
  }

  // A press that panned isn't a click. A click on the background clears
  // the selection.
  function onClickCapture(event) {
    if (gesture.current?.moved) {
      event.stopPropagation();
      gesture.current.moved = false;
    }
  }
  function onBackgroundClick(event) {
    if (!event.target.closest(".topo-node, .topo-controls")) onSelect?.(null);
  }

  // ---- Keyboard ----

  const [active, setActive] = useState(null);
  const firstCard = view.nodes[0]?.id;
  const activeId = placed.nodes[active] ? active : placed.nodes[selected] ? selected : placed.nodes[focus] ? focus : firstCard;

  function onKeyDown(event) {
    if (event.altKey || event.ctrlKey || event.metaKey) return;
    const onCard = event.target.closest?.(".topo-node");
    if (onCard && event.key.startsWith("Arrow")) {
      const next = neighbour(placed, onCard.dataset.id, event.key);
      event.preventDefault();
      if (next) focusCard(next);
      return;
    }
    switch (event.key) {
      case "+":
      case "=":
        zoomCentre(1.25);
        break;
      case "-":
        zoomCentre(0.8);
        break;
      case "0":
        fit();
        break;
      case "Home":
        if (focus && placed.nodes[focus]) focusCard(focus);
        break;
      case "Escape":
        if (!selected) return;
        onSelect?.(null);
        break;
      default:
        return;
    }
    event.preventDefault();
  }

  // ---- Drawing ----

  const near = new Set(selected ? placed.edges.filter((e) => e.from === selected || e.to === selected).map((e) => e.id) : []);
  const transform = t && `translate(${t.x}px, ${t.y}px) scale(${t.k})`;

  return (
    <div
      ref={viewportRef}
      className={`topo ${panning ? "is-panning" : ""}`}
      role="group"
      aria-roledescription="map"
      aria-label={label}
      aria-describedby={hintId}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      onClickCapture={onClickCapture}
      onClick={onBackgroundClick}
      onKeyDown={onKeyDown}
      style={t ? { backgroundPosition: `${t.x}px ${t.y}px`, backgroundSize: `${20 * t.k}px ${20 * t.k}px` } : undefined}
    >
      <p id={hintId} className="visually-hidden">
        Arrow keys move along connections, Enter shows a card's details, + and − zoom, 0 fits the map on screen.
      </p>
      {view.nodes.length === 0 && <p className="topo-empty">Nothing to show.</p>}
      <div className="topo-canvas" style={{ transform, visibility: t ? "visible" : "hidden" }}>
        <svg className="topo-edges" width={placed.width} height={placed.height} aria-hidden="true" focusable="false">
          {placed.edges.map((e) => (
            <path
              key={e.id}
              d={e.path}
              className={[
                "topo-edge",
                `topo-edge-${e.type}`,
                e.optional && "topo-edge-optional",
                near.has(e.id) && "topo-edge-near",
              ]
                .filter(Boolean)
                .join(" ")}
            />
          ))}
          {placed.edges
            .filter((e) => lit.has(e.id))
            .map((e) => {
              const { severity, reverse } = lit.get(e.id);
              return (
                <g key={e.id} className={`topo-chain topo-chain-${severity}`}>
                  <path d={e.path} className="topo-chain-line" />
                  <path d={e.path} className={`topo-chain-flow ${reverse ? "topo-chain-reverse" : ""}`} />
                </g>
              );
            })}
        </svg>
        {view.nodes.map((n) => (
          <Card
            key={n.id}
            n={n}
            state={shownState(n, index)}
            box={placed.nodes[n.id]}
            namespace={namespace}
            isFocus={n.id === focus}
            isSelected={n.id === selected}
            root={roots.get(n.id)}
            tabIndex={n.id === activeId ? 0 : -1}
            cardRef={(el) => (el ? cards.current.set(n.id, el) : cards.current.delete(n.id))}
            onClick={() => {
              setActive(n.id);
              onSelect?.(n.id);
            }}
            onFocus={() => {
              setActive(n.id);
              showCard(n.id);
            }}
          />
        ))}
      </div>

      {/* Column headings sit just above the cards, and stay in sight when
          the map pans down. */}
      {t && (
        <div
          className={`topo-columns ${headingsTop(t) < 0 ? "is-stuck" : ""}`}
          style={{ top: Math.max(0, headingsTop(t)) }}
          aria-hidden="true"
        >
          {placed.columns.map((c) => (
            <div key={c.key} className="topo-column" style={{ left: c.x * t.k + t.x, width: NODE_W * t.k }}>
              {c.label}
            </div>
          ))}
        </div>
      )}

      <div className="topo-controls">
        <Button size="sm" icon={PlusIcon} label="Zoom in" onClick={() => zoomCentre(1.25)} />
        <Button size="sm" icon={MinusIcon} label="Zoom out" onClick={() => zoomCentre(0.8)} />
        <Button size="sm" icon={FitIcon} label="Fit on screen" onClick={fit} />
        {focus && placed.nodes[focus] && (
          <Button
            size="sm"
            icon={CrosshairIcon}
            label="Show this object"
            onClick={() => {
              focusCard(focus);
              centreCard(focus);
            }}
          />
        )}
      </div>
    </div>
  );
}

// Card is one object: its kind, its name and its status.
function Card({ n, state, box, namespace, isFocus, isSelected, root, tabIndex, cardRef, onClick, onFocus }) {
  const Icon = kindIcon(n.resource);
  const name = cardName(n, namespace);
  const kind = kindText(n);
  const status = statusText(n, state);
  const words = [`${kind} ${name}`, `${STATE_WORDS[state]}${status && status !== STATE_WORDS[state] ? `: ${status}` : ""}`];
  if (root) words.push("root cause");
  if (isFocus) words.push("this object");

  return (
    <button
      ref={cardRef}
      type="button"
      data-id={n.id}
      className={[
        "topo-node",
        `topo-${state}`,
        n.pods && "topo-group",
        isFocus && "topo-focus",
        isSelected && "topo-selected",
      ]
        .filter(Boolean)
        .join(" ")}
      style={{ left: box.x, top: box.y, width: box.w, height: box.h }}
      tabIndex={tabIndex}
      aria-label={words.join(", ")}
      aria-pressed={isSelected}
      title={[`${n.kind} ${n.namespace ? `${n.namespace}/` : ""}${n.name}`, n.message].filter(Boolean).join("\n")}
      onClick={onClick}
      onFocus={onFocus}
    >
      <span className="topo-kind">
        <Icon size={14} />
        <span className="topo-kind-name">{kind}</span>
        {n.state === "missing" && <span className="topo-tag topo-tag-bad">missing</span>}
        {n.state === "unknown" && <span className="topo-tag">can't read</span>}
        {root && n.state !== "missing" && <span className={`topo-tag topo-tag-${root === "failing" ? "bad" : "warn"}`}>root cause</span>}
      </span>
      <span className="topo-name">{name}</span>
      <span className="topo-status">
        <StateMark state={state} />
        <span className="topo-status-text">{status}</span>
      </span>
    </button>
  );
}

export function StateMark({ state }) {
  if (state === "failing" || state === "missing") return <ErrorIcon size={12} className="topo-mark topo-mark-bad" />;
  if (state === "degraded") return <WarningIcon size={12} className="topo-mark topo-mark-warn" />;
  return <span className={`topo-dot topo-dot-${state}`} aria-hidden="true" />;
}

// kindText is a card's kind: "Deployment", or for a group "3 Pods".
export function kindText(n) {
  return n.pods ? `${n.pods.total} ${n.pods.total === 1 ? "Pod" : "Pods"}` : n.kind;
}

// cardName is the name on a card, with the namespace when it isn't the one
// being looked at.
export function cardName(n, namespace) {
  return n.namespace && namespace && n.namespace !== namespace ? `${n.namespace}/${n.name}` : n.name;
}

// statusText is what a card says about its state: its status, or the
// state in words (state is the one shown, if worse than its own).
export function statusText(n, state = n.state) {
  if (n.state === "missing") return "doesn't exist";
  if (n.state === "unknown") return "can't read";
  return n.status || STATE_WORDS[state];
}

// neighbour picks the card an arrow key moves to: along an edge to the
// left or right (the nearest by height), or the next card up or down in the
// same column. With no edge that way, the nearest card in the next column.
export function neighbour(placed, id, key) {
  const here = placed.nodes[id];
  const all = Object.values(placed.nodes);
  if (key === "ArrowUp" || key === "ArrowDown") {
    const column = all.filter((n) => n.rank === here.rank).sort((a, b) => a.y - b.y);
    const i = column.findIndex((n) => n.id === id);
    return column[i + (key === "ArrowDown" ? 1 : -1)]?.id;
  }
  const right = key === "ArrowRight";
  const ahead = (n) => (right ? n.rank > here.rank : n.rank < here.rank);
  const linked = placed.edges
    .flatMap((e) => (e.from === id ? [e.to] : e.to === id ? [e.from] : []))
    .map((other) => placed.nodes[other])
    .filter(ahead);
  const pool = linked.length ? linked : all.filter((n) => n.rank === here.rank + (right ? 1 : -1));
  pool.sort(
    (a, b) =>
      Math.abs(a.rank - here.rank) - Math.abs(b.rank - here.rank) ||
      Math.abs(a.y - here.y) - Math.abs(b.y - here.y) ||
      (a.id < b.id ? -1 : 1),
  );
  return pool[0]?.id;
}

// headingsTop is where the column headings go on screen: ending where the
// space above the cards does.
const HEADINGS_H = 28;
function headingsTop(t) {
  return t.y + HEADER_H * t.k - HEADINGS_H + 4;
}

// firstView shows the whole map if it can be read that way. Otherwise it
// shows the focused card at a readable size, as near the middle as it can
// without leaving space beside the map that it could fill.
function firstView(placed, size, focus) {
  const whole = fitAll(placed, size);
  const card = placed.nodes[focus] ?? Object.values(placed.nodes)[0];
  if (whole.k >= READABLE || !card) return whole;
  const k = READABLE;
  const fill = (want, room, length) => (length * k + 2 * PAD <= room ? (room - length * k) / 2 : clamp(want, room - PAD - length * k, PAD));
  return {
    k,
    x: fill(size.w / 2 - (card.x + NODE_W / 2) * k, size.w, placed.width),
    y: fill(size.h / 2 - (card.y + NODE_H / 2) * k, size.h, placed.height),
  };
}

// fitAll fits the map on screen, never larger than its real size.
function fitAll(placed, size) {
  const k = clamp(Math.min((size.w - 2 * PAD) / (placed.width || 1), (size.h - 2 * PAD) / (placed.height || 1), 1), MIN_ZOOM, 1);
  return { k, x: (size.w - placed.width * k) / 2, y: Math.max(PAD, (size.h - placed.height * k) / 2) };
}

function clamp(v, lo, hi) {
  return Math.min(hi, Math.max(lo, v));
}
