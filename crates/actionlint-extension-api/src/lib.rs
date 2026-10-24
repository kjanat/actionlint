#![doc = include_str!("../README.md")]

/// Generated types, guest traits and host imports from the canonical WIT contract.
pub mod bindings {
    #[cfg(not(feature = "source-annotations"))]
    wit_bindgen::generate!({
        path: "wit",
        world: "plugin",
        pub_export_macro: true,
    });

    #[cfg(feature = "source-annotations")]
    wit_bindgen::generate!({
        path: "wit",
        world: "annotated-plugin",
        pub_export_macro: true,
    });
}

pub use bindings::{exports, kjanat};
pub use exports::kjanat::actionlint::analyzer::*;

/// Canonical analyzer interface implemented by this SDK.
pub const ANALYZER_INTERFACE: &str = "kjanat:actionlint/analyzer@0.1.0";
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
