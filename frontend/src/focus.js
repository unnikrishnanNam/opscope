// focusSection scrolls a part of the page (a card, the diagnosis) to the
// top of the content and moves keyboard focus to it, so the next Tab
// continues from there rather than from the top of the page.
//
// It scrolls only the nearest box that scrolls (the content area, or the
// whole page on phones). scrollIntoView would also move the boxes around it
// that hide their overflow, shifting the top bar out of view.
export function focusSection(id) {
  const section = document.getElementById(id);
  if (!section) return;
  section.setAttribute("tabindex", "-1");
  section.focus({ preventScroll: true });

  let box = section.parentElement;
  while (box && !/(auto|scroll)/.test(getComputedStyle(box).overflowY)) box = box.parentElement;
  const gap = 16;
  if (box) box.scrollTo({ top: box.scrollTop + section.getBoundingClientRect().top - box.getBoundingClientRect().top - gap });
  else window.scrollTo({ top: window.scrollY + section.getBoundingClientRect().top - gap });
}

// focusSectionSoon is focusSection for a part that's about to appear, such
// as the diagnosis right after switching to the Summary tab, which the
// router renders a moment later. It looks again every 50 ms for up to a
// second. (Timers rather than animation frames: browsers pause those in a
// tab that isn't on screen.)
export function focusSectionSoon(id, tries = 20) {
  if (document.getElementById(id)) focusSection(id);
  else if (tries > 0) setTimeout(() => focusSectionSoon(id, tries - 1), 50);
}
