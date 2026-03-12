"use client";

import { useCallback, useEffect, useRef, useState } from "react";

/** Prefix for all localStorage keys used by this app. */
const KEY_PREFIX = "pc:";

/**
 * A React hook that synchronizes state with localStorage.
 *
 * Hydration-safe: initializes from `defaultValue` on the server and during the
 * first client render, then loads any stored client value after mount.
 *
 * @param key - The localStorage key (automatically prefixed with "pc:").
 * @param defaultValue - The value to use when nothing is stored or on parse error.
 * @returns The stateful value, a setter that updates both React state and localStorage,
 * and a flag that becomes true once the initial client-side storage read completes.
 */
export function useLocalStorage<T>(
  key: string,
  defaultValue: T
): [T, (value: T | ((prev: T) => T)) => void, boolean] {
  const prefixedKey = KEY_PREFIX + key;
  const defaultValueRef = useRef(defaultValue);
  const [storedValue, setStoredValue] = useState<T>(defaultValue);
  const [isLoaded, setIsLoaded] = useState(false);

  useEffect(() => {
    defaultValueRef.current = defaultValue;
  }, [defaultValue]);

  useEffect(() => {
    if (typeof window === "undefined") {
      return;
    }

    try {
      const item = window.localStorage.getItem(prefixedKey);
      if (item !== null) {
        setStoredValue(JSON.parse(item) as T);
      }
    } catch {
      setStoredValue(defaultValueRef.current);
    } finally {
      setIsLoaded(true);
    }
  }, [prefixedKey]);

  const setValue = useCallback(
    (value: T | ((prev: T) => T)) => {
      setStoredValue((prev) => {
        const nextValue =
          value instanceof Function ? value(prev) : value;
        if (typeof window !== "undefined") {
          try {
            window.localStorage.setItem(
              prefixedKey,
              JSON.stringify(nextValue)
            );
          } catch {
            // localStorage quota exceeded or unavailable — state still updates
          }
        }
        return nextValue;
      });
    },
    [prefixedKey]
  );

  return [storedValue, setValue, isLoaded];
}
