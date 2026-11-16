# syntax=docker/dockerfile:1
# Production build of the web demo, served by nginx. /v1 and /healthz are
# proxied to the API (see nginx.conf), so the page and the API share an origin.
#   docker build -t nabu-web web/
# Normally started by docker compose (repository root): docker compose up --build

FROM node:22-bookworm-slim AS build
WORKDIR /web
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /web/dist /usr/share/nginx/html
EXPOSE 80
