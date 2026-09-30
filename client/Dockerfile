# syntax=docker/dockerfile:1
FROM oven/bun:1.3.11 AS deps
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

# Runtime needs `next` and react only — not typescript, eslint or the compiler.
FROM oven/bun:1.3.11 AS prod-deps
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production

# Build + run on node: `bun run build` segfaults inside bun 1.3.11 on linux/arm64
# (Bun's own crash, not next's). bun is still what installs the lockfile.
FROM node:22-bookworm-slim AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
# NEXT_PUBLIC_* are inlined into the bundle at BUILD time, not read at runtime.
# Changing them requires a rebuild (docker compose build client).
ARG NEXT_PUBLIC_PRIVY_APP_ID
ARG NEXT_PUBLIC_PRIVY_SIGNER_ID
ARG NEXT_PUBLIC_PRIVY_POLICY_ID
ARG NEXT_PUBLIC_CHAIN_ID=143
ARG NEXT_PUBLIC_RPC_URL=https://rpc.monad.xyz
ARG NEXT_PUBLIC_API_URL=http://localhost:8080
ARG NEXT_PUBLIC_MARKET_DATA_URL=http://localhost:8082
ARG NEXT_PUBLIC_MARKET_DATA_WS_URL=ws://localhost:8082/api/v1/ws
ARG NEXT_PUBLIC_HISTORY_URL=http://localhost:8087
ENV NEXT_PUBLIC_PRIVY_APP_ID=$NEXT_PUBLIC_PRIVY_APP_ID \
    NEXT_PUBLIC_PRIVY_SIGNER_ID=$NEXT_PUBLIC_PRIVY_SIGNER_ID \
    NEXT_PUBLIC_PRIVY_POLICY_ID=$NEXT_PUBLIC_PRIVY_POLICY_ID \
    NEXT_PUBLIC_CHAIN_ID=$NEXT_PUBLIC_CHAIN_ID \
    NEXT_PUBLIC_RPC_URL=$NEXT_PUBLIC_RPC_URL \
    NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL \
    NEXT_PUBLIC_MARKET_DATA_URL=$NEXT_PUBLIC_MARKET_DATA_URL \
    NEXT_PUBLIC_MARKET_DATA_WS_URL=$NEXT_PUBLIC_MARKET_DATA_WS_URL \
    NEXT_PUBLIC_HISTORY_URL=$NEXT_PUBLIC_HISTORY_URL \
    NEXT_TELEMETRY_DISABLED=1
RUN ./node_modules/.bin/next build && rm -rf .next/cache

FROM node:22-bookworm-slim AS runner
WORKDIR /app
ENV NODE_ENV=production PORT=3000 NEXT_TELEMETRY_DISABLED=1
# no standalone output (next.config.ts targets Vercel): run `next start` on prod deps
COPY --from=prod-deps /app/node_modules ./node_modules
COPY --from=build /app/.next ./.next
COPY --from=build /app/public ./public
COPY package.json next.config.ts ./
EXPOSE 3000
USER node
CMD ["node", "node_modules/next/dist/bin/next", "start"]
