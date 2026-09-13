import { useSyncExternalStore } from "react";

function subscribe(callback: () => void) {
  window.addEventListener("hashchange", callback);
  return () => window.removeEventListener("hashchange", callback);
}
export function useRoute() {
  const value = useSyncExternalStore(subscribe, () => window.location.hash);
  return new URLSearchParams(value.slice(1));
}
export function link(values: Record<string, string>) {
  return `#${new URLSearchParams(values).toString()}`;
}
export function navigate(values: Record<string, string>) {
  window.location.hash = link(values);
}
