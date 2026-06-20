package com.alpr;

import org.bytedeco.javacv.FFmpegFrameGrabber;
import org.bytedeco.javacv.Frame;
import org.bytedeco.javacv.Java2DFrameConverter;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.*;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.time.Instant;
import java.util.UUID;

/**
 * RTSP ALPR Poller — Java
 * Uses JavaCV (FFmpeg bindings) to grab frames and Java 11 HttpClient to POST to the ALPR API.
 *
 * Build:  mvn package
 * Run:    RTSP_URL=rtsp://... java -jar target/rtsp-alpr-poller-1.0.0.jar
 */
public class RtspPoller {

    public static void main(String[] args) throws Exception {
        String rtspUrl   = "rtsp://{username}:{password}@{your_ip}:{port}/Streaming/channels/101";
        String apiKey    = "your key, if any";
        String apiBase   = "your api url";
        String country   = "IND";
        long   intervalMs = 2000;

        System.out.printf("starting java poller — rtsp: %s, interval: %dms%n", rtspUrl, intervalMs);

        HttpClient http = HttpClient.newBuilder()
            .connectTimeout(Duration.ofSeconds(5))
            .build();

        FFmpegFrameGrabber grabber = new FFmpegFrameGrabber(rtspUrl);
        grabber.setOption("rtsp_transport", "tcp");
        grabber.start();

        Java2DFrameConverter converter = new Java2DFrameConverter();

        while (true) {
            Instant t0 = Instant.now();
            try {
                // drain buffer — grab several frames to get a fresh one
                Frame frame = null;
                for (int i = 0; i < 4; i++) {
                    frame = grabber.grabImage();
                }
                if (frame == null) throw new IOException("null frame");

                BufferedImage img = converter.convert(frame);
                byte[] jpeg = toJpeg(img);

                String boundary = UUID.randomUUID().toString();
                byte[] body = buildMultipart(boundary, country, jpeg);

                HttpRequest req = HttpRequest.newBuilder()
                    .uri(URI.create(apiBase + "/plate/recognize"))
                    .header("x-api-key", apiKey)
                    .header("Content-Type", "multipart/form-data; boundary=" + boundary)
                    .POST(HttpRequest.BodyPublishers.ofByteArray(body))
                    .timeout(Duration.ofSeconds(10))
                    .build();

                HttpResponse<String> resp = http.send(req, HttpResponse.BodyHandlers.ofString());
                long elapsed = Duration.between(t0, Instant.now()).toMillis();

                if (resp.statusCode() >= 300) {
                    System.err.printf("[error] api %d%n", resp.statusCode());
                } else {
                    System.out.printf("[%dms] %s%n", elapsed, resp.body());
                }
            } catch (Exception e) {
                System.err.printf("[error] %s%n", e.getMessage());
                grabber.restart();
            }

            long elapsed = Duration.between(t0, Instant.now()).toMillis();
            long sleep = intervalMs - elapsed;
            if (sleep > 0) Thread.sleep(sleep);
        }
    }

    private static byte[] toJpeg(BufferedImage img) throws IOException {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        ImageIO.write(img, "jpg", out);
        return out.toByteArray();
    }

    private static byte[] buildMultipart(String boundary, String country, byte[] jpeg) throws IOException {
        String sep = "--" + boundary + "\r\n";
        String end = "--" + boundary + "--\r\n";
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        PrintStream p = new PrintStream(out);

        p.print(sep);
        p.print("Content-Disposition: form-data; name=\"image\"; filename=\"frame.jpg\"\r\n");
        p.print("Content-Type: image/jpeg\r\n\r\n");
        out.write(jpeg);
        p.print("\r\n");

        p.print(sep);
        p.print("Content-Disposition: form-data; name=\"country\"\r\n\r\n");
        p.print(country + "\r\n");

        p.print(end);
        return out.toByteArray();
    }
}
