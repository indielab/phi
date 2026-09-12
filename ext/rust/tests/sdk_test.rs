//! End-to-end SDK tests: a fake PXB host drives the example binaries over
//! piped stdin/stdout and asserts the full handshake + RPC lifecycle.

use std::io::{BufReader, BufWriter, Write};
use std::process::{Child, ChildStdin, ChildStdout, Command, Stdio};

use phi_ext::pxb;

/// A minimal PXB host. Uses the crate's own codec, so byte-level fidelity to
/// the Go host is pinned separately in `tests/pxb_test.rs` (golden fixtures).
struct Host {
    child: Child,
    rd: BufReader<ChildStdout>,
    wr: BufWriter<ChildStdin>,
}

impl Host {
    fn spawn(name: &str) -> Self {
        let mut child = Command::new(example_bin(name))
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit())
            .spawn()
            .expect("spawn example binary");
        let rd = BufReader::new(child.stdout.take().unwrap());
        let wr = BufWriter::new(child.stdin.take().unwrap());
        Self { child, rd, wr }
    }

    fn read(&mut self) -> pxb::Frame {
        match pxb::read_frame(&mut self.rd) {
            Ok(f) => f,
            Err(e) => panic!("host read failed (extension crashed?): {e}"),
        }
    }

    fn write(&mut self, typ: u16, flags: u16, id: u32, body: &[u8]) {
        pxb::write_frame(&mut self.wr, typ, flags, id, body).unwrap();
        self.wr.flush().unwrap();
    }

    /// Completes the handshake for any extension: read `Hello`, reply
    /// `HelloAck`, then return the registration frames up to (and including)
    /// `Ready`.
    fn handshake(&mut self) -> pxb::Hello {
        let f = self.read();
        assert_eq!(f.header.typ, pxb::TYPE_HELLO);
        let hello = pxb::Hello::decode(&f.body).unwrap();
        self.write(
            pxb::TYPE_HELLO_ACK,
            0,
            0,
            &pxb::HelloAck {
                protocol: pxb::PROTOCOL_VERSION,
                phi_version: "v0.0.0-test".into(),
                cwd: "/tmp".into(),
                session_id: "s1".into(),
                extension_dir: "/ext".into(),
            }
            .encode(),
        );
        loop {
            let f = self.read();
            match pxb::FrameType::from_u16(f.header.typ) {
                pxb::FrameType::RegisterCommand
                | pxb::FrameType::RegisterTool
                | pxb::FrameType::Subscribe => {}
                pxb::FrameType::Ready => break,
                other => panic!("unexpected frame during registration: {other:?}"),
            }
        }
        hello
    }

    fn shutdown(&mut self) {
        self.write(pxb::TYPE_SHUTDOWN, 0, 0, &[]);
        let f = self.read();
        assert_eq!(f.header.typ, pxb::TYPE_SHUTDOWN_ACK);
        let status = self.child.wait().expect("wait for extension exit");
        assert!(status.success(), "extension exited with {status:?}");
    }
}

#[test]
fn hello_extension_lifecycle() {
    let mut h = Host::spawn("hello");

    let hello = h.handshake();
    assert_eq!(hello.name, "hello");
    assert_eq!(hello.version, "0.1.0");
    assert_eq!(hello.protocol, pxb::PROTOCOL_VERSION);
    assert_eq!(
        hello.caps,
        pxb::CAP_COMMANDS | pxb::CAP_INTERCEPT | pxb::CAP_EVENTS
    );

    // Command invoke → Notify frame, then CommandResponse echoing id.
    h.write(
        pxb::TYPE_COMMAND_INVOKED,
        pxb::FLAG_HAS_ID,
        1,
        &pxb::CommandInvoked {
            name: "hello".into(),
            args: "world".into(),
        }
        .encode(),
    );
    let f = h.read();
    assert_eq!(f.header.typ, pxb::TYPE_NOTIFY);
    let n = pxb::NotifyMsg::decode(&f.body).unwrap();
    assert_eq!((n.level.as_str(), n.message.as_str()), ("info", "Hello!"));

    let f = h.read();
    assert_eq!(f.header.typ, pxb::TYPE_COMMAND_RESPONSE);
    assert_eq!(f.header.flags & pxb::FLAG_HAS_ID, pxb::FLAG_HAS_ID);
    assert_eq!(f.header.id, 1);
    let resp = pxb::CommandResponse::decode(&f.body).unwrap();
    assert!(resp.ok);
    assert!(resp.error.is_empty());
    assert!(resp.submit.is_empty());

    // Intercept with no handler decision → empty response, id echoed.
    h.write(
        pxb::TYPE_INTERCEPT,
        pxb::FLAG_HAS_ID,
        2,
        &pxb::InterceptReq {
            event: pxb::Event::UserInput.code(),
            prompt: "hi".into(),
            ..Default::default()
        }
        .encode(),
    );
    let f = h.read();
    assert_eq!(f.header.typ, pxb::TYPE_INTERCEPT_RESPONSE);
    assert_eq!(f.header.flags & pxb::FLAG_HAS_ID, pxb::FLAG_HAS_ID);
    assert_eq!(f.header.id, 2);
    assert_eq!(f.body, pxb::InterceptResp::default().encode());

    // Fire-and-forget Event + SessionMeta must not break the loop.
    h.write(
        pxb::TYPE_EVENT,
        0,
        0,
        &pxb::EventNotify {
            event: pxb::Event::SessionStart.code(),
            session_id: "s9".into(),
            ..Default::default()
        }
        .encode(),
    );
    h.write(
        pxb::TYPE_SESSION_META,
        0,
        0,
        &pxb::SessionMeta {
            session_id: "s9".into(),
            cwd: "/new".into(),
        }
        .encode(),
    );

    h.shutdown();
}

#[test]
fn full_extension_confirm_tool_and_submit() {
    let mut h = Host::spawn("full");

    let hello = h.handshake();
    assert_eq!(hello.name, "full");
    assert_eq!(hello.caps, pxb::CAP_COMMANDS | pxb::CAP_TOOLS);

    // Command "ask" issues a confirm HostRequest (id 1), waits for the
    // HostResult, then notifies and replies with the queued submit.
    h.write(
        pxb::TYPE_COMMAND_INVOKED,
        pxb::FLAG_HAS_ID,
        7,
        &pxb::CommandInvoked {
            name: "ask".into(),
            args: String::new(),
        }
        .encode(),
    );

    let f = h.read();
    assert_eq!(f.header.typ, pxb::TYPE_HOST_REQUEST);
    assert_eq!(f.header.flags & pxb::FLAG_HAS_ID, pxb::FLAG_HAS_ID);
    assert_eq!(f.header.id, 1);
    let hr = pxb::HostRequest::decode(&f.body).unwrap();
    assert_eq!(hr.method, "confirm");
    assert!(
        hr.arg.contains(r#""Title":"Confirm?""#),
        "unexpected arg: {hr:?}"
    );
    assert!(
        hr.arg.contains(r#""Message":"Proceed with /tmp/x?""#),
        "unexpected arg: {hr:?}"
    );

    h.write(
        pxb::TYPE_HOST_RESULT,
        pxb::FLAG_HAS_ID,
        1,
        &pxb::HostResult {
            ok: true,
            ..Default::default()
        }
        .encode(),
    );

    let f = h.read();
    assert_eq!(f.header.typ, pxb::TYPE_NOTIFY);
    let n = pxb::NotifyMsg::decode(&f.body).unwrap();
    assert_eq!(
        (n.level.as_str(), n.message.as_str()),
        ("info", "Confirmed!")
    );

    let f = h.read();
    assert_eq!(f.header.typ, pxb::TYPE_COMMAND_RESPONSE);
    assert_eq!(f.header.id, 7);
    let resp = pxb::CommandResponse::decode(&f.body).unwrap();
    assert!(resp.ok);
    assert_eq!(resp.submit, "follow-up from ask");

    // Tool invoke echoes its own id.
    h.write(
        pxb::TYPE_TOOL_INVOKE,
        pxb::FLAG_HAS_ID,
        9,
        &pxb::ToolInvoke {
            name: "echo".into(),
            args: br#"{"text":"hi"}"#.to_vec(),
        }
        .encode(),
    );
    let f = h.read();
    assert_eq!(f.header.typ, pxb::TYPE_TOOL_RESULT);
    assert_eq!(f.header.id, 9);
    let tr = pxb::ToolResultMsg::decode(&f.body).unwrap();
    assert!(!tr.is_error);
    assert_eq!(tr.content, r#"echo: {"text":"hi"}"#);
    assert!(tr.error.is_empty());

    // DetailFromArgs RPC (same invoke body, lighter reply).
    h.write(
        pxb::TYPE_TOOL_DETAIL_INVOKE,
        pxb::FLAG_HAS_ID,
        10,
        &pxb::ToolInvoke {
            name: "echo".into(),
            args: br#"{"text":"hi"}"#.to_vec(),
        }
        .encode(),
    );
    let f = h.read();
    assert_eq!(f.header.typ, pxb::TYPE_TOOL_DETAIL_RESULT);
    assert_eq!(f.header.id, 10);
    let detail = pxb::ToolDetailResult::decode(&f.body).unwrap();
    assert_eq!(detail.detail, r#"{"text":"hi"}"#);

    // Async tool handler: the SDK drives the returned future to completion
    // on its single-threaded runtime (the handler yields once inside).
    h.write(
        pxb::TYPE_TOOL_INVOKE,
        pxb::FLAG_HAS_ID,
        11,
        &pxb::ToolInvoke {
            name: "async-echo".into(),
            args: br#"{"text":"yo"}"#.to_vec(),
        }
        .encode(),
    );
    let f = h.read();
    assert_eq!(f.header.typ, pxb::TYPE_TOOL_RESULT);
    assert_eq!(f.header.id, 11);
    let tr = pxb::ToolResultMsg::decode(&f.body).unwrap();
    assert!(!tr.is_error);
    assert_eq!(tr.content, r#"async echo: {"text":"yo"}"#);
    assert!(tr.error.is_empty());

    h.shutdown();
}

/// Resolves an example binary. `CARGO_BIN_EXE_<name>` is only set for `bin`
/// targets, so examples are located under the target dir at test runtime.
/// Scoped invocations (`cargo test --test sdk_test`, or a test runner that
/// executes the test binary directly) do not build examples, so build the
/// one we need on demand via the `CARGO` env var cargo embeds at compile time.
fn example_bin(name: &str) -> std::path::PathBuf {
    let manifest = env!("CARGO_MANIFEST_DIR");
    let mut dir = if let Ok(t) = std::env::var("CARGO_TARGET_DIR") {
        std::path::PathBuf::from(t)
    } else {
        std::path::PathBuf::from(manifest).join("target")
    };
    let profile = if cfg!(debug_assertions) {
        "debug"
    } else {
        "release"
    };
    dir.push(profile);
    dir.push("examples");
    #[cfg(windows)]
    let file = format!("{name}.exe");
    #[cfg(not(windows))]
    let file = name.to_string();
    dir.push(file);
    if !dir.exists() {
        let status = std::process::Command::new(env!("CARGO"))
            .current_dir(manifest)
            .args(["build", "--example", name])
            .status()
            .expect("failed to run cargo build --example");
        assert!(status.success(), "cargo build --example {name} failed");
    }
    dir
}
