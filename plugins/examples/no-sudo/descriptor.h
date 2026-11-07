/* String values must be valid C and JSON string literals. Both the component's
 * describe export and the distribution metadata expand these declarations. */
#define NO_SUDO_DESCRIPTOR(STRING, BOOL) \
    STRING(name, "no-sudo") \
    STRING(description, "Check runner privilege assumptions") \
    STRING(category, "suspicious") \
    STRING(subcategory, "scripts") \
    STRING(maturity, "stable") \
    STRING(provider, "example") \
    STRING(explanation, "Self-hosted runners may not provide sudo.") \
    STRING(help, "Run without privilege elevation or explicitly prepare the runner.") \
    STRING(documentation_url, "") \
    STRING(options_schema, "{}") \
    BOOL(recommended, false)

#define NO_SUDO_INPUTS(INPUT) \
    INPUT(SCRIPTS) \
    INPUT(INVOCATIONS)
