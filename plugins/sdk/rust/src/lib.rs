#![doc = include_str!("../README.md")]

/// Generated types, guest traits and host imports from the WIT interface definitions.
pub mod bindings {
    #[cfg(not(any(
        feature = "source-annotations",
        feature = "tool-integration",
        feature = "script-environments"
    )))]
    wit_bindgen::generate!({
        path: "wit",
        world: "plugin",
        pub_export_macro: true,
    });

    #[cfg(all(
        feature = "source-annotations",
        not(any(feature = "tool-integration", feature = "script-environments"))
    ))]
    wit_bindgen::generate!({
        path: "wit",
        world: "annotated-plugin",
        pub_export_macro: true,
    });

    #[cfg(all(feature = "tool-integration", not(feature = "script-environments")))]
    wit_bindgen::generate!({
        path: "wit",
        world: "managed-plugin",
        with: { "kjanat:actionlint-script/shell-script-inputs@0.3.0": generate },
        pub_export_macro: true,
    });

    #[cfg(all(
        feature = "script-environments",
        not(any(feature = "source-annotations", feature = "tool-integration"))
    ))]
    wit_bindgen::generate!({
        path: "wit",
        world: "environment-plugin",
        with: { "kjanat:actionlint-script/environments@0.3.0": generate },
        pub_export_macro: true,
    });

    #[cfg(all(
        feature = "script-environments",
        feature = "source-annotations",
        not(feature = "tool-integration")
    ))]
    wit_bindgen::generate!({
        path: "wit",
        world: "environment-annotated-plugin",
        with: { "kjanat:actionlint-script/environments@0.3.0": generate },
        pub_export_macro: true,
    });

    #[cfg(all(feature = "script-environments", feature = "tool-integration"))]
    wit_bindgen::generate!({
        path: "wit",
        world: "environment-managed-plugin",
        with: {
            "kjanat:actionlint-script/shell-script-inputs@0.3.0": generate,
            "kjanat:actionlint-script/environments@0.3.0": generate,
        },
        pub_export_macro: true,
    });
}

/// Guest exports use one namespace even when host imports have another version.
pub mod exports {
    pub mod kjanat {
        pub use crate::bindings::exports::kjanat::actionlint0_2_0 as actionlint;
    }
}

/// Host imports selected by the SDK features, independent of generated version suffixes.
pub mod kjanat {
    #[cfg(any(feature = "tool-integration", feature = "script-environments"))]
    pub use crate::bindings::kjanat::actionlint_script;
    pub mod actionlint {
        pub use crate::bindings::kjanat::actionlint0_2_0::*;
    }
}

pub use exports::kjanat::actionlint::analyzer::*;

/// Canonical analyzer interface implemented by this SDK.
pub const ANALYZER_INTERFACE: &str = "kjanat:actionlint/analyzer@0.2.0";
/// Versioned configuration profile accepted by shell analysis integrations.
pub const SHELLCHECK_CONFIGURATION_SCHEMA: &str =
    include_str!("../json/shellcheck-config-0.1.0.json");
/// Analyzer WIT source, independent of optional host imports.
pub const ANALYZER_WIT: &str = include_str!("../wit/analyzer.wit");
/// SHA-256 of the analyzer WIT source with LF line endings.
pub const ANALYZER_WIT_SHA256: &str = env!("ACTIONLINT_ANALYZER_WIT_SHA256");

/// Runner-provider interface used by snapshot-based runner extensions.
pub const RUNNER_PROVIDER_INTERFACE: &str = "kjanat:actionlint/runner-provider@0.2.0";
/// Runner-provider WIT source for guest binding generation.
pub const RUNNER_PROVIDER_WIT: &str = include_str!("../wit/runner.wit");
/// SHA-256 of the runner-provider WIT source with LF line endings.
pub const RUNNER_PROVIDER_WIT_SHA256: &str = env!("ACTIONLINT_RUNNER_WIT_SHA256");

/// Export the component using this crate's generated bindings.
#[macro_export]
macro_rules! export_plugin {
    ($plugin:ident) => {
        $crate::bindings::export!($plugin with_types_in $crate::bindings);
    };
}

#[cfg(feature = "package")]
pub mod package;
