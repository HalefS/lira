/* useLayoutEffect rather than useEffect for anything GSAP touches: it runs after
   the DOM is updated but before the browser paints, so a tween starting from
   opacity 0 or scaleX 0 is never painted in its final state first. With
   useEffect that flicker is visible on every navigation. */
const { useState, useMemo, useEffect, useLayoutEffect, useCallback, useRef } = React;

