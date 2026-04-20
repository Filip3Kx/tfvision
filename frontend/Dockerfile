# Build stage
FROM node:20-slim AS build
WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
RUN npm run build

# Final stage - just provides the files
FROM alpine:latest
COPY --from=build /app/dist /dist
# Copy files to mount point at runtime to handle volume initialization
CMD ["sh", "-c", "cp -rf /dist/* /app/dist/ 2>/dev/null || true; while true; do sleep 3600; done"]
