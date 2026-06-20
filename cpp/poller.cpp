/**
 * RTSP ALPR Poller — C++
 * Lowest overhead option; ideal for Pi Zero 2 W.
 * Uses OpenCV for frame capture and libcurl for multipart POST.
 *
 * Build:
 *   mkdir build && cd build
 *   cmake .. && make -j$(nproc)
 *
 * Usage:
 *   RTSP_URL=rtsp://... API_KEY=... COUNTRY=IND ./poller
 */

#include <opencv2/opencv.hpp>
#include <curl/curl.h>

#include <chrono>
#include <cstdlib>
#include <cstring>
#include <iostream>
#include <stdexcept>
#include <string>
#include <thread>
#include <vector>

static std::string env_or(const char* key, const char* def) {
    const char* v = std::getenv(key);
    return (v && *v) ? std::string(v) : std::string(def);
}

static std::string now_iso() {
    auto t = std::chrono::system_clock::now();
    auto tt = std::chrono::system_clock::to_time_t(t);
    char buf[32];
    std::strftime(buf, sizeof(buf), "%Y-%m-%dT%H:%M:%S", std::gmtime(&tt));
    return buf;
}

// libcurl write callback — accumulates response body
static size_t write_cb(void* ptr, size_t size, size_t nmemb, std::string* out) {
    out->append(static_cast<char*>(ptr), size * nmemb);
    return size * nmemb;
}

std::string recognize_plate(
    CURL* curl,
    const std::string& api_base,
    const std::string& api_key,
    const std::string& country,
    const std::vector<uchar>& jpeg)
{
    std::string url = api_base + "/plate/recognize";
    std::string response;

    curl_mime* mime = curl_mime_init(curl);

    curl_mimepart* part = curl_mime_addpart(mime);
    curl_mime_name(part, "image");
    curl_mime_filename(part, "frame.jpg");
    curl_mime_type(part, "image/jpeg");
    curl_mime_data(part, reinterpret_cast<const char*>(jpeg.data()), jpeg.size());

    curl_mimepart* cpart = curl_mime_addpart(mime);
    curl_mime_name(cpart, "country");
    curl_mime_data(cpart, country.c_str(), CURL_ZERO_TERMINATED);

    struct curl_slist* headers = nullptr;
    headers = curl_slist_append(headers, ("x-api-key: " + api_key).c_str());

    curl_easy_setopt(curl, CURLOPT_URL, url.c_str());
    curl_easy_setopt(curl, CURLOPT_MIMEPOST, mime);
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, write_cb);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &response);
    curl_easy_setopt(curl, CURLOPT_TIMEOUT, 10L);

    CURLcode rc = curl_easy_perform(curl);

    curl_slist_free_all(headers);
    curl_mime_free(mime);

    if (rc != CURLE_OK) {
        throw std::runtime_error(std::string("curl: ") + curl_easy_strerror(rc));
    }

    long http_code = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &http_code);
    if (http_code >= 300) {
        throw std::runtime_error("api error " + std::to_string(http_code));
    }

    return response;
}

int main() {
    std::string rtsp_url   = "rtsp://{username}:{password}@{your_ip}:{port}/Streaming/channels/101";
    std::string api_key    = "your key, if any";
    std::string api_base   = "your api url";
    std::string country    = "IND";
    int         interval_ms = 2000;

    std::cout << "starting c++ poller — rtsp: " << rtsp_url
              << ", interval: " << interval_ms << "ms\n";

    curl_global_init(CURL_GLOBAL_DEFAULT);
    CURL* curl = curl_easy_init();
    if (!curl) throw std::runtime_error("curl_easy_init failed");

    cv::VideoCapture cap;
    cap.open(rtsp_url, cv::CAP_FFMPEG);
    cap.set(cv::CAP_PROP_BUFFERSIZE, 1);
    if (!cap.isOpened()) {
        std::cerr << "cannot open stream: " << rtsp_url << "\n";
        return 1;
    }

    while (true) {
        auto t0 = std::chrono::steady_clock::now();

        try {
            // drain stale frames
            cv::Mat frame;
            for (int i = 0; i < 3; i++) cap.grab();
            if (!cap.retrieve(frame)) throw std::runtime_error("retrieve failed");

            std::vector<uchar> jpeg;
            std::vector<int> params = {cv::IMWRITE_JPEG_QUALITY, 85};
            cv::imencode(".jpg", frame, jpeg, params);

            std::string result = recognize_plate(curl, api_base, api_key, country, jpeg);
            auto ms = std::chrono::duration_cast<std::chrono::milliseconds>(
                std::chrono::steady_clock::now() - t0).count();
            std::cout << "[" << now_iso() << "] [" << ms << "ms] " << result << "\n";

        } catch (const std::exception& e) {
            std::cerr << "[error] " << e.what() << "\n";
            cap.release();
            std::this_thread::sleep_for(std::chrono::seconds(2));
            cap.open(rtsp_url, cv::CAP_FFMPEG);
        }

        auto elapsed = std::chrono::duration_cast<std::chrono::milliseconds>(
            std::chrono::steady_clock::now() - t0).count();
        long sleep = interval_ms - elapsed;
        if (sleep > 0) std::this_thread::sleep_for(std::chrono::milliseconds(sleep));
    }

    curl_easy_cleanup(curl);
    curl_global_cleanup();
    return 0;
}
