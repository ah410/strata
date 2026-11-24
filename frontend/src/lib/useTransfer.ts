import { useEffect, useRef, useState } from "react";
import { Rate, smoothStep } from "./rate";

/**
 * Speed and a smoothly advancing count for a transfer that reports `done`
 * in bursts (upload chunks, job polls). The shown count never passes done.
 */
export function useTransfer(done: number): { shown: number; speed: number } {
  const rate = useRef(new Rate());
  const [speed, setSpeed] = useState(0);
  const real = useRef(done);
  const [shown, setShown] = useState(done);

  useEffect(() => {
    real.current = done;
    rate.current.add(done);
  }, [done]);
  // Speed changes are read once a second so the numbers don't flicker.
  useEffect(() => {
    const t = window.setInterval(() => setSpeed(rate.current.perSecond()), 1000);
    return () => window.clearInterval(t);
  }, []);
  useEffect(() => {
    let last = performance.now();
    let frame = requestAnimationFrame(function tick(now) {
      setShown((s) => smoothStep(s, real.current, rate.current.perSecond(), now - last));
      last = now;
      frame = requestAnimationFrame(tick);
    });
    return () => cancelAnimationFrame(frame);
  }, []);

  return { shown, speed };
}
