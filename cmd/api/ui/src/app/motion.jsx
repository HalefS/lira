/* ════════════════ MOTION ════════════════ */
/*
  GSAP is used for three things only: the page transition, the dashboard
  counters, and the bars and gauges. Everything else in this file stays on CSS
  keyframes. A declarative fade or slide is not worth a library; what GSAP is
  actually here for is animating values that are only known at runtime -- a
  number that has been computed from the data, a bar whose length depends on the
  largest of a set of counts it has never seen before.

  Two rules keep GSAP and React from fighting over the same DOM node:

  1. Every animation is created inside a gsap.matchMedia() query and destroyed
     with mm.revert() in the effect cleanup. GSAP writes to the DOM directly and
     React owns the same nodes; without the teardown a tween outlives the
     component that started it and goes on writing to a detached element.

  2. Every animation is gated on prefers-reduced-motion. This file had no
     reduced-motion handling of any kind before this, so that is worth having
     for the animations at least. Under reduce(), no tween is ever created and
     the DOM is left exactly as React rendered it -- which means the components
     using these hooks must be able to render their final state on their own.
     They all can, except CountUp, which is why it renders empty and writes its
     own text.
*/

/* Shared tweens. Each takes gsap explicitly rather than reaching for the global,
   so a call site that passes null (GSAP absent, or motion reduced) is obvious. */
const MOTION = {
  /* A page arriving: a short lift and fade. Deliberately under 300ms and with no
     delay, so it reads as instant but not as a cut. */
  enter(g, el, opts = {}) {
    if (!g || opts.reduced || !el) return;
    g.fromTo(el, { opacity: 0, y: 10 },
      { opacity: 1, y: 0, duration: 0.28, ease: 'power2.out',
        overwrite: true, clearProps: 'transform' });
  },

  /* Bars and gauges are sized with an inline width or height, so the growth is a
     scale transform layered on top of that rather than a fight with the layout
     property itself. Capped, because a long list should settle quickly rather
     than crawl. */
  grow(g, els, axis = 'x', each = 0.04, cap = 40) {
    if (!g || !els) return;
    const list = Array.from(els).slice(0, cap);
    if (!list.length) return;
    const prop = axis === 'x' ? 'scaleX' : 'scaleY';
    g.fromTo(list, { [prop]: 0 },
      { [prop]: 1, duration: 0.55, ease: 'power2.out',
        stagger: { each, from: 'start' }, overwrite: true });
  },

  /* A reader's lights coming on, one day after another down the trial strip.

     Only the dots that carry a result are passed in, so the untested grey ones
     stay where they are and the eye is led along the days that actually
     happened. A failure is animated exactly like a pass -- what colour a dot ends
     up is CSS's business, not this -- so a red in the middle of a green run
     lights on in its own turn rather than being skipped or jumping the queue.

     back.out rather than power2.out, because the overshoot is the point: each
     light arrives with a small pop, so a fortnight of them reads as a run of
     little clicks rather than a slide. `delay` offsets whole strips against each
     other, so several readers fill in one after another instead of all at once. */
  light(g, dots, opts = {}) {
    if (!g || !dots) return;
    const list = Array.from(dots);
    if (!list.length) return;
    const { each = 0.055, delay = 0 } = opts;
    g.fromTo(list, { scale: 0.15, opacity: 0.2 },
      { scale: 1, opacity: 1, duration: 0.32, ease: 'back.out(2.6)',
        delay, stagger: { each, from: 'start' }, overwrite: true });
  },
};

/* Runs build(gsap, element, { reduced }) after the element is on the DOM, again
   whenever deps change, and cleans up after itself. Returns the ref to attach.

   build is always called, even when there is nothing to animate, so a component
   can still establish its final state. When motion is not wanted -- reduced
   motion asked for, or GSAP failed to load -- gsap is handed over as null, which
   every helper here already treats as "leave the DOM alone". That is what makes
   the gate a single decision in one place instead of a check at each call site. */
function useAnimate(build, deps = []) {
  const root = useRef(null);
  useLayoutEffect(() => {
    const el = root.current;
    if (!el) return;
    const g = window.gsap;
    const still = () => build(null, el, { reduced: true });
    if (!g) { still(); return; }
    const mm = g.matchMedia();
    mm.add({
      still:  '(prefers-reduced-motion: reduce)',
      motion: '(prefers-reduced-motion: no-preference)',
    }, (ctx) => {
      if (ctx.conditions.still) still();
      else build(g, el, { reduced: false });
    });
    return () => mm.revert();
  }, deps);
  return root;
}

/* Counts a number up when its value changes.

   The span is rendered with no children on purpose. If React owned this text,
   GSAP's onUpdate would replace the text node while React still held a
   reference to the original, and the next React render would write to the
   detached one -- leaving the number frozen on screen. Here exactly one owner
   writes it, which is this function, on every path including the reduced one. */
function CountUp({ value, decimals = 0, className, style }) {
  const n = Number(value);
  const target = Number.isFinite(n) ? n : 0;
  const ref = useAnimate((g, el, opts) => {
    const final = target.toFixed(decimals);
    // The finished number is written before anything is animated, so the span is
    // never empty. It used to be written only from GSAP's onUpdate, and a tween
    // that is created but never advances -- GSAP's ticker is suspended in a
    // background tab, where requestAnimationFrame does not fire -- left the span
    // blank for good: a tile with a label and no number on it. React still renders
    // no children, so this stays the only thing that ever writes the text, and the
    // animation still gets to run: it starts on the next frame, which lands before
    // the browser paints, so the value written here is never itself shown.
    el.textContent = final;
    if (!g || opts.reduced) return;
    const proxy = { n: 0 };
    g.to(proxy, {
      n: target, duration: 0.8, ease: 'power2.out',
      onUpdate:    () => { el.textContent = proxy.n.toFixed(decimals); },
      onComplete:  () => { el.textContent = final; },
    });
  }, [target, decimals]);
  return <span ref={ref} className={className} style={style}/>;
}

/* ════════════════ PAGE TRANSITION ════════════════ */
/*
  Wraps the page area so changing page cross-fades instead of cutting.

  The wrapper element itself stays mounted and only the animation re-runs, keyed
  on `page`. Remounting the wrapper with a React key would work too, but it would
  throw away and rebuild the whole subtree on every navigation for no reason.
*/
function PageShell({ page, children }) {
  const ref = useAnimate((g, root, opts) => {
    // Animate the rendered page, not the wrapper: animating the wrapper would
    // also fade anything else ever placed alongside the page in here.
    MOTION.enter(g, root.firstElementChild, opts);
  }, [page]);
  return <div ref={ref}>{children}</div>;
}

