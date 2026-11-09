fn descriptor() -> Descriptor {
    Descriptor {
        id: "example".into(), version: "1.0.0".into(), name: "Example".into(),
        engine: "example".into(), engine_version: "1.0.0".into(), shells: vec!["python".into()],
        rule: RuleDescriptor {
            name: "example".into(), description: "Example rule".into(), category: "correctness".into(),
            subcategory: "python".into(), maturity: "stable".into(), provider: "example".into(),
            recommended: true, explanation: String::new(), help: String::new(),
            documentation_url: String::new(), options_schema: r#"{"type":"object"}"#.into(), examples: vec![],
        },
        #[cfg(feature = "v03")]
        capabilities: Capabilities { fixes: true, reported_sources: true, resources: vec![ResourceKind::Directory] },
    }
}
