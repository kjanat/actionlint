#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "gen/plugin.h"

typedef exports_kjanat_actionlint_analyzer_position_t position_t;
typedef exports_kjanat_actionlint_analyzer_script_t script_t;
typedef exports_kjanat_actionlint_analyzer_diagnostic_t diagnostic_t;

typedef struct {
    diagnostic_t *ptr;
    size_t len;
    size_t cap;
} findings_t;

static bool position(const script_t *script, uint32_t line, uint32_t column, bool exclusive, position_t *out) {
    for (size_t i = 0; i < script->spans.len; i++) {
        const exports_kjanat_actionlint_analyzer_span_t *span = &script->spans.ptr[i];
        if (span->line != line) {
            continue;
        }
        bool inside = column >= span->start && column < span->end;
        if (exclusive) {
            inside = (column > span->start && column <= span->end) || (span->start == span->end && column == span->start);
        }
        if (!inside) {
            continue;
        }
        if (span->document_end.is_some) {
            *out = exclusive && column == span->end ? span->document_end.val : span->document;
            return true;
        }
        out->line = span->document.line;
        out->column = span->document.column + column - span->start;
        return true;
    }
    return false;
}

static void report(findings_t *findings, const script_t *script, uint32_t line, uint32_t column) {
    position_t start;
    position_t end;
    if (!position(script, line, column, false, &start)) {
        return;
    }
    if (!position(script, line, column + 4, true, &end)) {
        end = start;
    }
    if (findings->len == findings->cap) {
        findings->cap = findings->cap ? findings->cap * 2 : 4;
        findings->ptr = realloc(findings->ptr, findings->cap * sizeof(diagnostic_t));
        if (!findings->ptr) {
            abort();
        }
    }
    diagnostic_t *d = &findings->ptr[findings->len++];
    memset(d, 0, sizeof(*d));
    d->reference.is_some = true;
    plugin_string_dup_n(&d->reference.val.document, (const char *)script->reference.document.ptr, script->reference.document.len);
    plugin_string_dup_n(&d->reference.val.revision, (const char *)script->reference.revision.ptr, script->reference.revision.len);
    plugin_string_dup_n(&d->reference.val.node, (const char *)script->reference.node.ptr, script->reference.node.len);
    plugin_string_dup_n(&d->reference.val.address, (const char *)script->reference.address.ptr, script->reference.address.len);
    plugin_string_dup(&d->severity, "warning");
    plugin_string_dup(&d->message, "step runs sudo, which self-hosted runners may not provide");
    plugin_string_dup_n(&d->path, (const char *)script->path.ptr, script->path.len);
    d->start = start;
    d->end = end;
}

static bool word_ends(const uint8_t *text, size_t len, size_t at) {
    if (at >= len) {
        return true;
    }
    uint8_t c = text[at];
    return c == ' ' || c == '\t' || c == '\n' || c == ';' || c == '|' || c == '&';
}

static size_t scalar_width(uint8_t lead) {
    if (lead >= 0xF0) {
        return 4;
    }
    if (lead >= 0xE0) {
        return 3;
    }
    if (lead >= 0xC0) {
        return 2;
    }
    return 1;
}

static void check_script(const script_t *script, findings_t *findings) {
    const uint8_t *text = script->text.ptr;
    size_t len = script->text.len;
    uint32_t line = 1;
    uint32_t column = 1;
    bool command_start = true;
    for (size_t i = 0; i < len; i += scalar_width(text[i]), column++) {
        uint8_t c = text[i];
        if (c == '\n') {
            line++;
            column = 0;
            command_start = true;
        } else if (c == ' ' || c == '\t') {
            continue;
        } else if (c == ';' || c == '|' || c == '&') {
            command_start = true;
        } else if (command_start && len - i >= 4 && memcmp(text + i, "sudo", 4) == 0 && word_ends(text, len, i + 4)) {
            report(findings, script, line, column);
            command_start = false;
        } else {
            command_start = false;
        }
    }
}

void exports_kjanat_actionlint_analyzer_describe(exports_kjanat_actionlint_analyzer_descriptor_t *ret) {
    memset(ret, 0, sizeof(*ret));
    plugin_string_dup(&ret->name, "no-sudo");
    plugin_string_dup(&ret->description, "Check runner privilege assumptions");
    plugin_string_dup(&ret->category, "portability");
    plugin_string_dup(&ret->subcategory, "scripts");
    plugin_string_dup(&ret->maturity, "stable");
    plugin_string_dup(&ret->provider, "example");
    plugin_string_dup(&ret->explanation, "Self-hosted runners may not provide sudo.");
    plugin_string_dup(&ret->help, "Run without privilege elevation or explicitly prepare the runner.");
    plugin_string_dup(&ret->fix_availability, "none");
    plugin_string_dup(&ret->options_schema, "{}");
    ret->requires_.ptr = malloc(2 * sizeof(exports_kjanat_actionlint_analyzer_fact_t));
    if (!ret->requires_.ptr) {
        abort();
    }
    ret->requires_.ptr[0] = EXPORTS_KJANAT_ACTIONLINT_ANALYZER_FACT_SCRIPTS;
    ret->requires_.ptr[1] = EXPORTS_KJANAT_ACTIONLINT_ANALYZER_FACT_INVOCATIONS;
    ret->requires_.len = 2;
}

bool exports_kjanat_actionlint_analyzer_analyze(exports_kjanat_actionlint_analyzer_input_t *input,
                                                exports_kjanat_actionlint_analyzer_list_diagnostic_t *ret,
                                                plugin_string_t *err) {
    (void)err;
    findings_t findings = {0};
    for (size_t i = 0; i < input->scripts.len; i++) {
        check_script(&input->scripts.ptr[i], &findings);
    }
    for (size_t i = 0; i < input->invoked.len; i++) {
        check_script(&input->invoked.ptr[i], &findings);
    }
    exports_kjanat_actionlint_analyzer_input_free(input);
    ret->ptr = findings.ptr;
    ret->len = findings.len;
    return true;
}
