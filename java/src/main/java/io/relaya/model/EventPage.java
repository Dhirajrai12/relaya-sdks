package io.relaya.model;

import java.util.List;

/** API model; field names match the API in snake_case. */
public record EventPage(
        List<EventSummary> data,
        /** null on the last page */ String nextCursor) {
}
