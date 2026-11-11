import { createRemoteApi } from "./remote-api.ts";
export { ApiError } from "./api-error.ts";
export type Api = ReturnType<typeof createRemoteApi>;
export const api = createRemoteApi();
