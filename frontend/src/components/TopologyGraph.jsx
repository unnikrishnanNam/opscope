import { memo, useCallback, useEffect, useId, useImperativeHandle, useLayoutEffect, useMemo, useRef, useState } from "react";
import { LINE_STYLE, kindIcon } from "../graph/kinds.js";
import { HEADER_H, NODE_H, NODE_W, layout } from "../graph/layout.js";
import { cardKind } from "../graph/view.js";
import Button from "./Button.jsx";
import { CrosshairIcon, EyeOffIcon, FitIcon, MinusIcon, MissingIcon, PlusIcon, UnlinkIcon } from "./icons.jsx";
import "./TopologyGraph.css";

const MIN_ZOOM = 0.15;
const MAX_ZOOM = 2;
const READABLE = 0.75; // below this, the first view shows the focus rather than everything
const PAD = 24;
const DRAG = 4; // px a press must move before it pans instead of clicking

// TopologyGraph draws a shaped view of the graph (graph/view.js): cards in
// columns by kind, and the edges between them, each with an arrowhead at
// what it needs.
//
//   view      { nodes, edges } from shape()
//   focus     the object the map is about ("you are here"), if any
//   selected  the card whose details are open; onSelect(id or null)
//   label     names the map for screen readers
//   inset     px on the right covered by a panel, kept clear when showing a card
//   fitKey    the map is fitted on screen again when this changes
//   empty     what to say when there's nothing to draw
//   ref       { showCard(id), focusCard(id), fit() }
//
// Drag or scroll to pan; zoom with ⌘/Ctrl and the wheel, a pinch, the
// buttons, or + and −. Arrow keys move along edges, Enter opens a card.
export default function TopologyGraph({ view, focus, selected, onSelect, label, inset = 0, fitKey, empty = "Nothing to show.", ref }) {
  const hintId = useId();
  const viewportRef = useRef(null);
  const canvasRef = useRef(null);
  const cards = useRef(new Map());

  // Cards keep their order from one refresh to the next.
  const previous = useRef(null);
  const placed = useMemo(() => layout(view, { previous: previous.current?.order }), [view]);
  useEffect(() => {
    previous.current = placed;
  }, [placed]);

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

  // The width not covered by a panel; on a narrow screen the panel covers
  // the map, so all of it.
  const free = size ? size.w - (inset < size.w * 0.6 ? inset : 0) : 0;

  useLayoutEffect(() => {
    if (!size || (t && fitted.current === fitKey)) return;
    fitted.current = fitKey;
    setT(firstView(placed, { w: free, h: size.h }, focus ?? selected));
  }, [size, free, fitKey, placed, focus, selected, t]);

  const zoomAt = useCallback((factor, cx, cy) => {
    setT((t) => {
      const k = clamp(t.k * factor, MIN_ZOOM, MAX_ZOOM);
      return { k, x: cx - ((cx - t.x) * k) / t.k, y: cy - ((cy - t.y) * k) / t.k };
    });
  }, []);
  const zoomCentre = (factor) => size && zoomAt(factor, free / 2, size.h / 2);
  const zoomTo = (k) => t && zoomCentre(k / t.k);
  const fit = () => size && setT(fitAll(placed, { ...size, w: free }));

  // Keeps a card on screen, moving as little as possible.
  const showCard = useCallback(
    (id) => {
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
        if (top < PAD + COLUMNS_H) dy = PAD + COLUMNS_H - top;
        else if (bottom > size.h - PAD - CONTROLS_H) dy = Math.max(size.h - PAD - CONTROLS_H - bottom, PAD - top);
        return dx || dy ? { ...t, x: t.x + dx, y: t.y + dy } : t;
      });
    },
    [placed, size, free],
  );

  // Puts a card in the middle of what's free of the panel.
  function centreCard(id) {
    const n = placed.nodes[id];
    if (!n || !size) return;
    setT((t) => ({ ...t, x: free / 2 - (n.x + n.w / 2) * t.k, y: size.h / 2 - (n.y + n.h / 2) * t.k }));
  }

  const [active, setActive] = useState(null);
  const focusCard = useCallback(
    (id) => {
      setActive(id);
      cards.current.get(id)?.focus({ preventScroll: true });
      showCard(id);
    },
    [showCard],
  );

  useImperativeHandle(ref, () => ({ showCard, focusCard, fit }));

  // A card picked on the map is kept clear of the panel its picking opens.
  useEffect(() => {
    if (selected && t) showCard(selected);
    // Only when the selection or the panel changes, not on every pan.
  }, [selected, inset]);

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
        setT((t) => t && { ...t, x: t.x - event.deltaX * unit, y: t.y - event.deltaY * unit });
      }
    }
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [zoomAt]);

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

  const firstCard = Object.values(placed.nodes).sort((a, b) => a.rank - b.rank || a.y - b.y)[0]?.id;
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
    if (event.target.closest?.(".topo-controls")) return; // the buttons keep their keys
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

  const [hovered, setHovered] = useState(null);
  const onCardClick = useCallback(
    (id) => {
      setActive(id);
      onSelect?.(id);
    },
    [onSelect],
  );
  const onCardFocus = useCallback(
    (id) => {
      setActive(id);
      showCard(id);
    },
    [showCard],
  );
  const cardRef = useCallback((id, el) => (el ? cards.current.set(id, el) : cards.current.delete(id)), []);

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
      style={t ? { backgroundPosition: `${t.x}px ${t.y}px`, backgroundSize: `${gridStep(t.k)}px ${gridStep(t.k)}px` } : undefined}
    >
      <p id={hintId} className="visually-hidden">
        Arrow keys move along connections and up and down a column. Enter shows a card's details, Escape closes them. Plus
        and minus zoom, 0 fits the map on screen.
      </p>
      {view.nodes.length === 0 && <p className="topo-empty">{empty}</p>}
      <div ref={canvasRef} className="topo-canvas" style={{ transform, visibility: t ? "visible" : "hidden" }}>
        <MapContent
          view={view}
          placed={placed}
          focus={focus}
          selected={selected}
          hovered={hovered}
          activeId={activeId}
          onCardClick={onCardClick}
          onCardFocus={onCardFocus}
          onHover={setHovered}
          cardRef={cardRef}
        />
      </div>

      {/* Column headings sit just above the cards, and stay in sight when
          the map is panned down. */}
      {t && placed.columns.length > 0 && (NODE_W + COLUMN_GAP) * t.k >= MIN_HEADING_PX && (
        <div className={`topo-columns ${headingsTop(t) < 0 ? "is-stuck" : ""}`} style={{ top: Math.max(0, headingsTop(t)) }} aria-hidden="true">
          {placed.columns.map((c, i) => (
            // A heading may use the gap beside its column, so it fits when zoomed out.
            <div
              key={c.key}
              className="topo-column"
              style={{ left: c.x * t.k + t.x, width: Math.max(0, ((placed.columns[i + 1]?.x ?? c.x + NODE_W) - c.x) * t.k - 8) }}
            >
              {c.label}
            </div>
          ))}
        </div>
      )}

      {view.nodes.length > 0 && (
        <div className="topo-controls" role="toolbar" aria-label="Zoom">
          <Button size="sm" variant="quiet" icon={MinusIcon} label="Zoom out" onClick={() => zoomCentre(0.8)} />
          <button type="button" className="topo-zoom" onClick={() => zoomTo(1)} title="Zoom to 100%">
            <span className="visually-hidden">Zoom: </span>
            {t ? `${Math.round(t.k * 100)}%` : "–"}
          </button>
          <Button size="sm" variant="quiet" icon={PlusIcon} label="Zoom in" onClick={() => zoomCentre(1.25)} />
          <span className="topo-controls-sep" aria-hidden="true" />
          <Button size="sm" variant="quiet" icon={FitIcon} label="Fit on screen" onClick={fit} />
          {focus && placed.nodes[focus] && (
            <Button
              size="sm"
              variant="quiet"
              icon={CrosshairIcon}
              label="Show this object"
              onClick={() => {
                focusCard(focus);
                centreCard(focus);
              }}
            />
          )}
        </div>
      )}
    </div>
  );
}

// MapContent is everything inside the canvas: the edges and the cards. It's
// kept apart, and memoized, so panning and zooming (which only move the
// canvas) never draw a thousand cards again.
const MapContent = memo(function MapContent({
  view,
  placed,
  focus,
  selected,
  hovered,
  activeId,
  onCardClick,
  onCardFocus,
  onHover,
  cardRef,
}) {
  // Marker IDs are referred to from CSS-like url(#…), so they keep to plain characters.
  const markerId = `topo-arrow-${useId().replace(/[^a-zA-Z0-9_-]/g, "")}`;
  // What's joined to the selected card, which stays bright while the rest
  // dims; and the hovered card's edges, drawn stronger.
  const near = useMemo(() => {
    const around = (id) => {
      const edges = new Set();
      const nodes = new Set(id ? [id] : []);
      if (!id) return { edges, nodes };
      for (const e of placed.edges) {
        if (e.from === id || e.to === id) {
          edges.add(e.id);
          nodes.add(e.from);
          nodes.add(e.to);
        }
      }
      return { edges, nodes };
    };
    return { selected: around(selected), hovered: around(hovered) };
  }, [placed, selected, hovered]);
  // The base layer draws every edge plain and never changes with the
  // pointer. The selected and hovered cards' edges are drawn again, strong,
  // on a layer of their own. While a card is selected, a veil goes over the
  // map, and only what's joined to it (and a card the pointer is on) sits
  // above it. So hovering or selecting changes a few elements, not all.
  const dimming = Boolean(selected && placed.nodes[selected]);
  const top = useMemo(
    () => placed.edges.filter((e) => near.selected.edges.has(e.id) || near.hovered.edges.has(e.id)),
    [placed, near],
  );
  return (
    <>
      <svg className="topo-edges" width={placed.width} height={placed.height} aria-hidden="true" focusable="false">
        <defs>
          {["plain", "strong"].map((kind) => (
            <marker
              key={kind}
              id={`${markerId}-${kind}`}
              className={`topo-arrow topo-arrow-${kind}`}
              viewBox="0 0 10 10"
              refX="9.5"
              refY="5"
              markerWidth="7"
              markerHeight="7"
              markerUnits="userSpaceOnUse"
              orient="auto-start-reverse"
            >
              <path d="M 0 0.5 L 10 5 L 0 9.5 z" />
            </marker>
          ))}
        </defs>
        <EdgePaths edges={placed.edges} strong={EMPTY} markerId={markerId} />
      </svg>
      {dimming && (
        <div
          className="topo-veil"
          style={{ left: -VEIL_PAD, top: -VEIL_PAD, width: placed.width + 2 * VEIL_PAD, height: placed.height + 2 * VEIL_PAD }}
          aria-hidden="true"
        />
      )}
      {top.length > 0 && (
        <svg className="topo-edges topo-edges-top" width={placed.width} height={placed.height} aria-hidden="true" focusable="false">
          <EdgePaths edges={top} strong={ALL} markerId={markerId} />
        </svg>
      )}
      {view.nodes.map((n) =>
        placed.nodes[n.id] ? (
          <Card
            key={n.id}
            n={n}
            box={placed.nodes[n.id]}
            isFocus={n.id === focus}
            isSelected={n.id === selected}
            isNear={dimming && (near.selected.nodes.has(n.id) || n.id === hovered)}
            tabIndex={n.id === activeId ? 0 : -1}
            cardRef={cardRef}
            onClick={onCardClick}
            onFocus={onCardFocus}
            onHover={onHover}
          />
        ) : null,
      )}
    </>
  );
});

const EMPTY = new Set();
const ALL = { has: () => true };
// How far the veil reaches past the map: well past any view, even zoomed
// out to the least, so its edge never shows as a line across the dots.
const VEIL_PAD = 8000;

// EdgePaths draws edges: strong ones (in `strong`) darker and thicker.
const EdgePaths = memo(function EdgePaths({ edges, strong, markerId }) {
  const arrow = (isStrong) => `url(#${markerId}-${isStrong ? "strong" : "plain"})`;
  return edges.map((e) => {
    const isStrong = strong.has(e.id);
    return (
      <path
        key={e.id}
        d={e.path}
        className={[
          "topo-edge",
          `topo-edge-${LINE_STYLE[e.kind] ?? "solid"}`,
          e.optional && "topo-edge-optional",
          isStrong && "topo-edge-strong",
        ]
          .filter(Boolean)
          .join(" ")}
        markerStart={e.toLeft ? arrow(isStrong) : undefined}
        markerEnd={e.toLeft ? undefined : arrow(isStrong)}
      />
    );
  });
});

// Card is one object: its kind, its name and its status, and a tag for a
// gap (missing, can't be read, selects nothing).
const Card = memo(function Card({ n, box, isFocus, isSelected, isNear, tabIndex, cardRef, onClick, onFocus, onHover }) {
  const Icon = kindIcon(n.resource);
  const tag = gapTag(n);
  const status = statusText(n);
  return (
    <button
      ref={(el) => cardRef(n.id, el)}
      type="button"
      data-id={n.id}
      className={[
        "topo-node",
        `topo-${n.presence ?? "present"}`,
        n.pods && "topo-group",
        isFocus && "topo-focus",
        isSelected && "topo-selected",
        isNear && "topo-near",
      ]
        .filter(Boolean)
        .join(" ")}
      style={{ left: box.x, top: box.y, width: box.w, height: box.h }}
      tabIndex={tabIndex}
      aria-label={cardLabel(n, isFocus)}
      aria-pressed={isSelected}
      title={fullName(n)}
      onClick={() => onClick(n.id)}
      onFocus={() => onFocus(n.id)}
      onPointerEnter={() => onHover(n.id)}
      onPointerLeave={() => onHover(null)}
    >
      <span className="topo-kind">
        <Icon size={14} className="topo-kind-icon" />
        <span className="topo-kind-name">{cardKind(n)}</span>
        {tag && (
          <span className={`topo-tag topo-tag-${tag.key}`}>
            <tag.Icon size={12} />
            {tag.label}
          </span>
        )}
      </span>
      <span className="topo-name">{n.name}</span>
      <span className="topo-status">{status || " "}</span>
    </button>
  );
});

// gapTag is the tag a card carries for a gap in the graph, if any.
export function gapTag(n) {
  if (n.presence === "missing") return { key: "missing", label: "Missing", Icon: MissingIcon };
  if (n.presence === "unknown") return { key: "unknown", label: "Can't read", Icon: EyeOffIcon };
  if (n.dangling?.length) return { key: "dangling", label: "Selects nothing", Icon: UnlinkIcon };
  return null;
}

// statusText is a card's last line: its status as its list page shows it,
// or what Opscope knows instead.
export function statusText(n) {
  if (n.presence === "missing") return "Doesn't exist";
  if (n.presence === "unknown") return n.resource ? "Not allowed to read" : "Kind not read by Opscope";
  if (n.endpoints) {
    const { ready, notReady } = n.endpoints;
    return notReady ? `${ready} ready, ${notReady} not ready` : `${ready} ready ${ready === 1 ? "endpoint" : "endpoints"}`;
  }
  if (n.dangling?.length) return "Matches no pods";
  return n.status ?? "";
}

// fullName is "Kind namespace/name", for tooltips.
export function fullName(n) {
  const name = n.namespace ? `${n.namespace}/${n.name}` : n.name;
  return n.pods ? `${cardKind(n)} of ${n.pods.ownerKind} ${name}` : `${n.kind} ${name}`;
}

// cardLabel is what a screen reader says for a card.
export function cardLabel(n, isFocus) {
  const words = [fullName(n)];
  const tag = gapTag(n);
  if (tag) words.push(tag.label.toLowerCase());
  const status = statusText(n);
  if (status && n.presence === "present") words.push(status);
  if (isFocus) words.push("this object");
  return words.join(", ");
}

// neighbour picks the card an arrow key moves to: along an edge to the left
// or right (the nearest by height), or the next card up or down in the same
// column. With no edge that way, the nearest card in the next column.
export function neighbour(placed, id, key) {
  const here = placed.nodes[id];
  if (!here) return undefined;
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
    .filter((n) => n && ahead(n));
  const pool = linked.length ? linked : all.filter((n) => n.rank === here.rank + (right ? 1 : -1));
  pool.sort(
    (a, b) =>
      Math.abs(a.rank - here.rank) - Math.abs(b.rank - here.rank) ||
      Math.abs(a.y - here.y) - Math.abs(b.y - here.y) ||
      (a.id < b.id ? -1 : 1),
  );
  return pool[0]?.id;
}

// gridStep is the dot grid's spacing on screen: 24 px of the map, doubled
// while that's under 12 px, so zoomed far out the dots stay a calm texture
// rather than a moiré.
export function gridStep(k) {
  let step = 24 * k;
  while (step < 12) step *= 2;
  return step;
}

// Column headings hide when a column is too narrow on screen to read one.
const COLUMN_GAP = 72;
const MIN_HEADING_PX = 96;

// The column headings' band, and the zoom controls' corner, which a card
// shown by keyboard stays clear of.
const COLUMNS_H = 28;
const CONTROLS_H = 44;

// headingsTop is where the column headings go on screen: ending where the
// space above the cards does.
function headingsTop(t) {
  return t.y + HEADER_H * t.k - COLUMNS_H + 4;
}

// firstView shows the whole map if it can be read that way. Otherwise it
// shows the focused (or selected) card at a readable size, as near the
// middle as it can without leaving space beside the map that it could
// fill. With neither, it starts where reading starts: the first column, at
// the top left, from the highest card in sight so none starts cut off
// under the headings, unless that would leave the first column's top card
// below the screen. (A tall namespace's first columns can start well below
// its top, so the map's own corner may be empty.)
export function firstView(placed, size, anchor) {
  const whole = fitAll(placed, size);
  if (whole.k >= READABLE) return whole;
  const k = READABLE;
  const fill = (want, room, length) => (length * k + 2 * PAD <= room ? (room - length * k) / 2 : clamp(want, room - PAD - length * k, PAD));
  const card = placed.nodes[anchor];
  if (card) {
    return {
      k,
      x: fill(size.w / 2 - (card.x + NODE_W / 2) * k, size.w, placed.width),
      y: fill(size.h / 2 - (card.y + NODE_H / 2) * k, size.h, placed.height),
    };
  }
  const cards = Object.values(placed.nodes);
  const first = cards.sort((a, b) => a.rank - b.rank || a.y - b.y)[0];
  const x = fill(PAD - (first?.x ?? 0) * k, size.w, placed.width);
  let top = first?.y ?? 0;
  for (const c of cards) {
    const inSight = (c.x + NODE_W) * k + x > 0 && c.x * k + x < size.w;
    if (inSight && c.y < top && (first.y - c.y + NODE_H) * k + PAD + COLUMNS_H <= size.h - CONTROLS_H) top = c.y;
  }
  return { k, x, y: fill(PAD + COLUMNS_H - top * k, size.h, placed.height) };
}

// fitAll fits the map on screen, never larger than its real size.
export function fitAll(placed, size) {
  const k = clamp(
    Math.min((size.w - 2 * PAD) / (placed.width || 1), (size.h - 2 * PAD - CONTROLS_H) / (placed.height || 1), 1),
    MIN_ZOOM,
    1,
  );
  return { k, x: (size.w - placed.width * k) / 2, y: Math.max(PAD, (size.h - CONTROLS_H - placed.height * k) / 2) };
}

function clamp(v, lo, hi) {
  return Math.min(hi, Math.max(lo, v));
}
