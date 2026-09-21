import { useCallback, useEffect, useRef, useState } from "react";
import { api, message } from "../lib/api";
export function useResource<T>(path: string | null, revision: number = 0) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(!!path);
  const [nonce, setNonce] = useState(0);
  const previousPath = useRef(path);
  useEffect(() => {
    if (!path) {
      setData(null);
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    if (previousPath.current !== path) {
      setData(null);
      previousPath.current = path;
    }
    setLoading(true);
    setError("");
    api<T>(path, { signal: controller.signal })
      .then((value) => {
        if (!controller.signal.aborted) setData(value);
      })
      .catch((error) => {
        if (!controller.signal.aborted) {
          setError(message(error));
          setData(null);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [path, revision, nonce]);
  return {
    data,
    error,
    loading,
    reload: useCallback(() => setNonce((value) => value + 1), []),
  };
}
