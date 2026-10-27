#include "analyzer.h"
#include <stdlib.h>
#include <string.h>

bool exports_actionlint_extension_analysis_analyze(
    exports_actionlint_extension_analysis_request_t *input,
    exports_actionlint_extension_analysis_analysis_result_t *result,
    exports_actionlint_extension_analysis_failure_t *error) {
  (void)error;
  *result = (actionlint_extension_types_analysis_result_t){.snapshot =
                                                               input->snapshot};
  for (size_t i = 0; i < input->expressions.len; i++) {
    const actionlint_extension_types_expression_t *expression =
        &input->expressions.ptr[i];
    for (size_t j = 0; j < expression->text.len; j++) {
      const actionlint_extension_types_text_segment_t *segment =
          &expression->text.ptr[j];
      if (segment->origin.tag != ACTIONLINT_EXTENSION_TYPES_ORIGIN_TRANSFORMED)
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
          .severity = ACTIONLINT_EXTENSION_TYPES_SEVERITY_INFORMATION,
          .primary = segment->origin.val.transformed,
      };
      analyzer_string_dup(&diagnostic->rule, "test/transformed-expression");
      diagnostic->message.ptr = malloc(segment->text.len);
      if (segment->text.len && !diagnostic->message.ptr)
        __builtin_trap();
      diagnostic->message.len = segment->text.len;
      if (segment->text.len)
        memcpy(diagnostic->message.ptr, segment->text.ptr, segment->text.len);
    }
  }
  actionlint_extension_types_request_free(input);
  return true;
}
