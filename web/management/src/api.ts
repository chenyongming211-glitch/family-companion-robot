import { createMockApi } from "./mock-api.ts";
import { createRemoteApi } from "./remote-api.ts";
export { createMockApi, ApiError } from "./mock-api.ts";
export type { DemoStorage, MockApi } from "./mock-api.ts";
export const liveMode = import.meta.env?.VITE_MANAGEMENT_API_MODE === "live";
function browserStorage() {
  try {
    return globalThis.sessionStorage;
  } catch {
    return undefined;
  }
}
export const api = liveMode
  ? createRemoteApi()
  : createMockApi(browserStorage());
