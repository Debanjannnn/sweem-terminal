// hypersync is the only data source wired here and its /query returns 401
// unauthenticated — there is no degraded mode. envio auto-loads every file in
// this directory at startup, so this throws before any block is fetched.
if (!process.env.ENVIO_API_TOKEN) {
  throw new Error(
    "ENVIO_API_TOKEN is required: HyperSync rejects unauthenticated /query with 401. " +
      "Get one at https://envio.dev/app/api-tokens, put it in main/.env. See SETUP.md §1.",
  );
}
