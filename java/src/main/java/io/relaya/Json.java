package io.relaya;

import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.JavaType;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.PropertyNamingStrategies;
import com.fasterxml.jackson.databind.SerializationFeature;
import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;
import java.io.IOException;
import java.io.UncheckedIOException;

/** The SDK's JSON mapper: snake_case on the wire, camelCase in Java, unknown fields ignored. */
final class Json {
    static final ObjectMapper MAPPER = new ObjectMapper()
            .registerModule(new JavaTimeModule())
            .setPropertyNamingStrategy(PropertyNamingStrategies.SNAKE_CASE)
            .setSerializationInclusion(JsonInclude.Include.NON_NULL)
            .configure(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES, false)
            .configure(SerializationFeature.WRITE_DATES_AS_TIMESTAMPS, false);

    private Json() {
    }

    static <T> T read(byte[] data, Class<T> type) {
        try {
            return MAPPER.readValue(data, type);
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        }
    }

    static <T> T read(byte[] data, JavaType type) {
        try {
            return MAPPER.readValue(data, type);
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        }
    }

    static JsonNode tree(byte[] data) {
        try {
            return MAPPER.readTree(data);
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        }
    }

    static byte[] write(Object value) {
        try {
            return MAPPER.writeValueAsBytes(value);
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        }
    }
}
