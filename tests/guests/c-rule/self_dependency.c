#include "analyzer.h"
#include <stdlib.h>
#include <string.h>

static uint8_t lower_ascii(uint8_t c) {
  return c >= 'A' && c <= 'Z' ? (uint8_t)(c + ('a' - 'A')) : c;
}

static bool same_job(const analyzer_string_t *a, const analyzer_string_t *b) {
  if (a->len != b->len)
    return false;
  for (size_t i = 0; i < a->len; i++) {
    if (lower_ascii(a->ptr[i]) != lower_ascii(b->ptr[i]))
      return false;
  }
  return true;
}

static void incomplete(actionlint_extension_types_analysis_result_t *result,
                       actionlint_extension_types_unavailable_t reason) {
  result->completion.tag = ACTIONLINT_EXTENSION_TYPES_COMPLETION_INCOMPLETE;
  actionlint_extension_types_list_unavailable_t *reasons =
      &result->completion.val.incomplete;
  void *next =
      realloc(reasons->ptr, (reasons->len + 1) * sizeof(*reasons->ptr));
  if (!next)
    __builtin_trap();
  reasons->ptr = next;
  reasons->ptr[reasons->len++] = reason;
}

static bool scalar_needs(const actionlint_extension_types_request_t *input,
                         actionlint_extension_types_range_t range) {
  for (size_t i = 0; i < input->sources.len; i++) {
    const actionlint_extension_types_source_t *source = &input->sources.ptr[i];
    if (source->id != range.source)
      continue;
    const analyzer_string_t *text = &source->text;
    if (range.start >= range.end || range.end > text->len)
      return false;
    size_t line = range.start;
    while (line > 0 && text->ptr[line - 1] != '\n' &&
           text->ptr[line - 1] != '\r')
      line--;
    while (line < range.start && text->ptr[line] == ' ')
      line++;
    if (range.start - line < 6 || memcmp(text->ptr + line, "needs:", 6) != 0)
      return false;
    line += 6;
    while (line < range.start &&
           (text->ptr[line] == ' ' || text->ptr[line] == '\t'))
      line++;
    if (line != range.start)
      return false;
    size_t end = range.end;
    while (end < text->len && (text->ptr[end] == ' ' || text->ptr[end] == '\t'))
      end++;
    return end == text->len || text->ptr[end] == '\n' ||
           text->ptr[end] == '\r' || text->ptr[end] == '#';
  }
  return false;
}

static void removal_fix(actionlint_extension_types_diagnostic_t *diagnostic) {
  diagnostic->fixes.ptr = calloc(1, sizeof(*diagnostic->fixes.ptr));
  if (!diagnostic->fixes.ptr)
    __builtin_trap();
  diagnostic->fixes.len = 1;
  actionlint_extension_types_fix_t *fix = diagnostic->fixes.ptr;
  analyzer_string_dup(&fix->title, "Remove self-dependency");
  fix->applicability = ACTIONLINT_EXTENSION_TYPES_APPLICABILITY_REQUIRES_REVIEW;
  fix->edits.ptr = calloc(1, sizeof(*fix->edits.ptr));
  if (!fix->edits.ptr)
    __builtin_trap();
  fix->edits.len = 1;
  fix->edits.ptr[0].location = diagnostic->primary;
  analyzer_string_dup(&fix->edits.ptr[0].replacement, "[]");
}

bool exports_actionlint_extension_analysis_analyze(
    exports_actionlint_extension_analysis_request_t *input,
    exports_actionlint_extension_analysis_analysis_result_t *result,
    exports_actionlint_extension_analysis_failure_t *error) {
  (void)error;
  *result = (actionlint_extension_types_analysis_result_t){.snapshot =
                                                               input->snapshot};
  if (input->jobs.tag == ACTIONLINT_EXTENSION_TYPES_JOBS_UNAVAILABLE) {
    incomplete(result, input->jobs.val.unavailable);
    actionlint_extension_types_request_free(input);
    return true;
  }
  const actionlint_extension_types_list_job_t *jobs = &input->jobs.val.known;
  for (size_t i = 0; i < jobs->len; i++) {
    const actionlint_extension_types_job_t *job = &jobs->ptr[i];
    if (job->needs.tag == ACTIONLINT_EXTENSION_TYPES_DEPENDENCIES_UNAVAILABLE) {
      incomplete(result, job->needs.val.unavailable);
      continue;
    }
    const actionlint_extension_types_list_name_t *needs = &job->needs.val.known;
    for (size_t j = 0; j < needs->len; j++) {
      const actionlint_extension_types_name_t *dependency = &needs->ptr[j];
      if (!same_job(&job->name.value, &dependency->value))
        continue;
      size_t count = result->diagnostics.len + 1;
      if (count > SIZE_MAX / sizeof(*result->diagnostics.ptr))
        __builtin_trap();
      void *next = realloc(result->diagnostics.ptr,
                           count * sizeof(*result->diagnostics.ptr));
      if (!next)
        __builtin_trap();
      result->diagnostics.ptr = next;
      actionlint_extension_types_diagnostic_t *diagnostic =
          &result->diagnostics.ptr[result->diagnostics.len++];
      *diagnostic = (actionlint_extension_types_diagnostic_t){
          .severity = ACTIONLINT_EXTENSION_TYPES_SEVERITY_ERROR,
          .primary = dependency->location,
      };
      diagnostic->related.ptr = calloc(1, sizeof(*diagnostic->related.ptr));
      if (!diagnostic->related.ptr)
        __builtin_trap();
      diagnostic->related.len = 1;
      diagnostic->related.ptr[0].location = job->name.location;
      analyzer_string_dup(&diagnostic->related.ptr[0].message,
                          "Job declared here.");
      if (needs->len == 1 && scalar_needs(input, dependency->location))
        removal_fix(diagnostic);
      analyzer_string_dup(&diagnostic->rule, "workflow/self-dependency");
      analyzer_string_dup(&diagnostic->message,
                          "A job cannot depend on itself.");
    }
  }
  actionlint_extension_types_request_free(input);
  return true;
}
