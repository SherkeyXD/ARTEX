"use client";

import * as React from "react";

export interface VisibilityPollOptions {
  /** Whether polling is active. Defaults to true. */
  enabled?: boolean;
  /** Whether to execute callback immediately on mount / when enabled becomes true. Defaults to true. */
  immediate?: boolean;
  /** If true, executes the callback immediately when user returns to the tab. Defaults to true. */
  refetchOnFocus?: boolean;
}

/**
 * useVisibilityPoll runs an asynchronous or synchronous callback on an interval,
 * but automatically PAUSES when document.hidden is true (tab in background, window minimized, lock screen).
 * When user focuses/returns to the tab, it immediately invokes the callback once and resumes the interval.
 * It also guards against overlapping in-flight executions.
 */
export function useVisibilityPoll(
  fn: () => void | Promise<void>,
  intervalMs: number,
  options: VisibilityPollOptions = {},
) {
  const { enabled = true, immediate = true, refetchOnFocus = true } = options;

  const fnRef = React.useRef(fn);
  fnRef.current = fn;

  const inFlightRef = React.useRef(false);

  React.useEffect(() => {
    if (!enabled || intervalMs <= 0) return;

    let timer: ReturnType<typeof setInterval> | null = null;
    let isDisposed = false;

    const run = async () => {
      if (isDisposed || inFlightRef.current) return;
      if (typeof document !== "undefined" && document.hidden) return;

      inFlightRef.current = true;
      try {
        await fnRef.current();
      } catch {
        // Ignored; errors handled by caller
      } finally {
        inFlightRef.current = false;
      }
    };

    const startTimer = () => {
      if (timer != null) clearInterval(timer);
      timer = setInterval(run, intervalMs);
    };

    const stopTimer = () => {
      if (timer != null) {
        clearInterval(timer);
        timer = null;
      }
    };

    const handleVisibilityChange = () => {
      if (typeof document === "undefined") return;
      if (document.hidden) {
        stopTimer();
      } else {
        if (refetchOnFocus) {
          void run();
        }
        startTimer();
      }
    };

    if (typeof document !== "undefined") {
      document.addEventListener("visibilitychange", handleVisibilityChange);
      if (!document.hidden) {
        if (immediate) {
          void run();
        }
        startTimer();
      }
    } else {
      if (immediate) {
        void run();
      }
      startTimer();
    }

    return () => {
      isDisposed = true;
      stopTimer();
      if (typeof document !== "undefined") {
        document.removeEventListener("visibilitychange", handleVisibilityChange);
      }
    };
  }, [enabled, intervalMs, immediate, refetchOnFocus]);
}
