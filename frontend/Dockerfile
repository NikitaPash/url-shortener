# ── Stage 1: install dependencies ────────────────────────────────────────────
FROM node:22-alpine AS deps
WORKDIR /app
COPY package*.json ./
RUN npm ci

# ── Stage 2: build ────────────────────────────────────────────────────────────
FROM deps AS builder
COPY . .
RUN npm run build

# ── Stage 3: serve ────────────────────────────────────────────────────────────
FROM nginx:1.27-alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost/ || exit 1
