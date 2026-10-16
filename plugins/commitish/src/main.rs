//! Explicit read-only resolution provider; commitish owns consumer ref semantics.
use commitish::{Content, Profile};
use commitish_github::Github;
use serde::Deserialize;
use serde_json::{Value, json};
use std::io::{self, Read};

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Request {
    version: u32,
    repository: String,
    #[serde(rename = "ref")]
    reference: String,
    kind: String,
    expected_repository_id: Option<u64>,
}
fn read_request(input: impl Read) -> Result<(Request, Profile), Box<dyn std::error::Error>> {
    let mut data = Vec::new();
    input.take(65_537).read_to_end(&mut data)?;
    if data.len() > 65_536 {
        return Err("request exceeds 64 KiB".into());
    }
    let request: Request = serde_json::from_slice(&data)?;
    if request.version != 1 {
        return Err("unsupported request version".into());
    }
    let profile = match request.kind.as_str() {
        "action" => Profile::GithubAction,
        "workflow" => Profile::GithubWorkflow,
        _ => return Err("unsupported consumer".into()),
    };
    Ok((request, profile))
}
fn resolve() -> Result<Value, Box<dyn std::error::Error>> {
    let args: Vec<_> = std::env::args().skip(1).collect();
    let token = match args.as_slice() {
        [] => None,
        [flag, variable] if flag == "--token-env" => Some(std::env::var(variable)?),
        _ => return Err("usage: actionlint-resolver-commitish [--token-env NAME]".into()),
    };
    let (request, profile) = read_request(io::stdin())?;
    let lookup = Github::new(token.as_deref())?.resolve(
        &request.repository,
        request.expected_repository_id,
        profile,
        &request.reference,
    )?;
    let pin = lookup.pin()?;
    let (commit, tree) = match pin.content() {
        Content::Commit(id) => (Some(id.to_string()), None),
        Content::Tree(id) => (None, Some(id.to_string())),
    };
    Ok(
        json!({"version":1,"repository_id":pin.repository().id,"repository":pin.repository().name,
        "commit":commit,"tree":tree,"observed_at":pin.observed_at(),"evidence":lookup}),
    )
}
fn main() {
    let result = resolve().unwrap_or_else(|error| json!({"version":1,"error":error.to_string()}));
    println!("{}", result);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn preserves_consumer_and_expected_identity() {
        for (kind, profile) in [
            ("action", Profile::GithubAction),
            ("workflow", Profile::GithubWorkflow),
        ] {
            let data = json!({"version":1,"repository":"owner/repo","ref":"release","kind":kind,"expected_repository_id":123}).to_string();
            let (request, actual) = read_request(data.as_bytes()).unwrap();
            assert_eq!(actual, profile);
            assert_eq!(request.expected_repository_id, Some(123));
            assert_eq!(request.reference, "release");
        }
    }

    #[test]
    fn rejects_invalid_requests_before_network_access() {
        for data in [
            r#"{"version":2,"repository":"a/b","ref":"main","kind":"action"}"#,
            r#"{"version":1,"repository":"a/b","ref":"main","kind":"generic"}"#,
            r#"{"version":1,"repository":"a/b","ref":"main","kind":"action","token":"forbidden"}"#,
            "{}",
            "not json",
        ] {
            assert!(read_request(data.as_bytes()).is_err(), "{data}");
        }
        assert!(read_request(vec![b' '; 65_537].as_slice()).is_err());
    }
}
