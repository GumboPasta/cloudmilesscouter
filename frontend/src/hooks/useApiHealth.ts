import { useEffect, useState } from "react";
import { getHealth } from "../utils/api.ts";

export type ApiHealth = "loading" | "ok" | "error";

/** Pings GET /healthz once on mount to confirm the API is reachable. */
export function useApiHealth(): ApiHealth {
  const [health, setHealth] = useState<ApiHealth>("loading");

  useEffect(() => {
    let cancelled = false;
    getHealth()
      .then((res) => {
        if (!cancelled) {
          setHealth(res.status === "ok" ? "ok" : "error");
        }
      })
      .catch(() => {
        if (!cancelled) {
          setHealth("error");
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return health;
}
