/// RTSP ALPR Poller — Rust
/// Captures one JPEG frame per interval via ffmpeg subprocess and POSTs it to the ALPR API.
///
/// Usage (cross-compile for Pi with `cargo build --target aarch64-unknown-linux-gnu --release`):
///   RTSP_URL=rtsp://... API_KEY=... COUNTRY=IND ./poller

use std::process::Command;
use std::time::{Duration, Instant};
use std::env;
use std::fs;

use reqwest::multipart;

const FRAME_PATH: &str = "/tmp/alpr_frame.jpg";

fn env_or(key: &str, default: &str) -> String {
    env::var(key).unwrap_or_else(|_| default.to_string())
}

async fn capture_frame(rtsp_url: &str) -> Result<Vec<u8>, String> {
    let status = Command::new("ffmpeg")
        .args([
            "-rtsp_transport", "tcp",
            "-i", rtsp_url,
            "-vframes", "1",
            "-q:v", "2",
            "-y", FRAME_PATH,
        ])
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .status()
        .map_err(|e| format!("ffmpeg spawn failed: {e}"))?;

    if !status.success() {
        return Err(format!("ffmpeg exited with {}", status));
    }

    fs::read(FRAME_PATH).map_err(|e| format!("read frame: {e}"))
}

async fn recognize_plate(
    client: &reqwest::Client,
    api_base: &str,
    api_key: &str,
    country: &str,
    image_bytes: Vec<u8>,
) -> Result<serde_json::Value, String> {
    let part = multipart::Part::bytes(image_bytes)
        .file_name("frame.jpg")
        .mime_str("image/jpeg")
        .map_err(|e| format!("mime: {e}"))?;

    let form = multipart::Form::new()
        .part("image", part)
        .text("country", country.to_string());

    let resp = client
        .post(format!("{api_base}/plate/recognize"))
        .header("x-api-key", api_key)
        .multipart(form)
        .send()
        .await
        .map_err(|e| format!("request failed: {e}"))?;

    if !resp.status().is_success() {
        return Err(format!("api error {}", resp.status()));
    }

    resp.json::<serde_json::Value>()
        .await
        .map_err(|e| format!("parse json: {e}"))
}

#[tokio::main]
async fn main() {
    let rtsp_url    = "rtsp://{username}:{password}@{your_ip}:{port}/Streaming/channels/101";
    let api_key     = "your key, if any";
    let api_base    = "your api url";
    let country     = "IND";
    let interval_ms: u64 = 2000;

    println!("starting rust poller — rtsp: {rtsp_url}, interval: {interval_ms}ms");

    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(10))
        .build()
        .expect("build http client");

    loop {
        let t0 = Instant::now();

        match capture_frame(&rtsp_url).await {
            Err(e) => eprintln!("[error] capture: {e}"),
            Ok(bytes) => {
                match recognize_plate(&client, &api_base, &api_key, &country, bytes).await {
                    Err(e) => eprintln!("[error] recognize: {e}"),
                    Ok(result) => {
                        println!("[{}ms] {}", t0.elapsed().as_millis(), result);
                    }
                }
            }
        }

        let elapsed = t0.elapsed();
        let interval = Duration::from_millis(interval_ms);
        if elapsed < interval {
            tokio::time::sleep(interval - elapsed).await;
        }
    }
}
