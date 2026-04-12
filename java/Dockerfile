# Stage 1: Build
FROM gradle:8.10-jdk21-alpine AS builder
WORKDIR /app

# Cache dependencies first (layer caching)
COPY build.gradle settings.gradle ./
RUN gradle dependencies --no-daemon -q 2>/dev/null || true

COPY src ./src
RUN gradle bootJar --no-daemon -q

# Stage 2: Runtime
FROM eclipse-temurin:21-jre-alpine
WORKDIR /app

COPY --from=builder /app/build/libs/*.jar app.jar

ENTRYPOINT ["java", "-jar", "app.jar"]
