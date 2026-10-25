#![doc = include_str!("../README.md")]

/// Generated types, guest traits and host imports from the canonical WIT contract.
pub mod bindings {
    #[cfg(not(any(feature = "source-annotations", feature = "tool-integration")))]
    wit_bindgen::generate!({
        path: "wit",
        world: "plugin",
        pub_export_macro: true,
    });

    #[cfg(all(feature = "source-annotations", not(feature = "tool-integration")))]
    wit_bindgen::generate!({
        path: "wit",
        world: "annotated-plugin",
        pub_export_macro: true,
    });

    #[cfg(feature = "tool-integration")]
    wit_bindgen::generate!({
        path: "wit",
        world: "managed-plugin",
        pub_export_macro: true,
    });
}

pub use bindings::{exports, kjanat};
pub use exports::kjanat::actionlint::analyzer::*;

/// Canonical analyzer interface implemented by this SDK.
pub const ANALYZER_INTERFACE: &str = "kjanat:actionlint/analyzer@0.1.0";
/// Versioned configuration profile accepted by shell analysis integrations.
pub const SHELLCHECK_CONFIGURATION_SCHEMA: &str =
    include_str!("../schemas/shellcheck-config-0.1.0.json");
/// Analyzer contract source, independent of optional host imports.
pub const ANALYZER_WIT: &str = include_str!("../wit/analyzer.wit");
/// SHA-256 of the analyzer contract with LF line endings.
pub const ANALYZER_CONTRACT_SHA256: &str = env!("ACTIONLINT_ANALYZER_CONTRACT_SHA256");

/// Export the component using this crate's generated bindings.
#[macro_export]
macro_rules! export_plugin {
    ($plugin:ident) => {
        $crate::bindings::export!($plugin with_types_in $crate::bindings);
    };
}

#[cfg(feature = "package")]
pub mod package;
