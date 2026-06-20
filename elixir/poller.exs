# ─────────────────────────────────────────
# ALPR Poller — Elixir
# Camera: Prama PT-NC140D3-WNM(D2)
# ─────────────────────────────────────────
# Install: https://elixir-lang.org/install.html
#          mix local.hex && mix archive.install hex mix_install_preview
# Run:     elixir poller.exs

Mix.install([
  {:req, "~> 0.5"},
])

defmodule Poller do
  # ── Config (edit these)
  @rtsp_url    "rtsp://{username}:{password}@{your_ip}:{port}/Streaming/channels/101"
  @api_key     "your key, if any"
  @api_base    "your api url"
  @country     "IND"
  @interval_ms 2_000
  @cooldown_ms 8_000
  @max_retries 3

  @curr_frame "/tmp/alpr_curr.jpg"
  @prev_frame "/tmp/alpr_prev.jpg"

  # ── Capture one JPEG frame via ffmpeg
  defp capture_frame do
    if File.exists?(@curr_frame), do: File.copy!(@curr_frame, @prev_frame)

    case System.cmd("ffmpeg", [
      "-loglevel", "error",
      "-rtsp_transport", "tcp",
      "-i", @rtsp_url,
      "-frames:v", "1",
      "-q:v", "3",
      "-vf", "scale=640:480",
      @curr_frame, "-y"
    ], stderr_to_stdout: true) do
      {_, 0} ->
        current  = File.read!(@curr_frame)
        previous = if File.exists?(@prev_frame), do: File.read!(@prev_frame), else: nil
        {:ok, current, previous}

      {out, code} ->
        {:error, "ffmpeg exited #{code}: #{out}"}
    end
  end

  # ── Motion detection: byte diff score
  defp detect_motion(_current, nil), do: false
  defp detect_motion(current, previous) do
    if :crypto.hash(:md5, current) == :crypto.hash(:md5, previous) do
      false
    else
      len     = min(byte_size(current), byte_size(previous))
      indices = Enum.take_every(0..(len - 1), 10)
      diff    = Enum.reduce(indices, 0.0, fn i, acc ->
        acc + abs(:binary.at(current, i) - :binary.at(previous, i))
      end)
      score = diff / length(indices) / 255.0 * 100.0
      IO.puts("[motion] #{Float.round(score, 2)}%")
      score > 30
    end
  end

  # ── POST frame to ALPR API
  defp scan_plate do
    image = File.read!(@curr_frame)
    resp  = Req.post!(
      "#{@api_base}/plate/recognize",
      headers: [{"x-api-key", @api_key}],
      form_multipart: [
        image:   {image, filename: "frame.jpg", content_type: "image/jpeg"},
        country: @country,
      ]
    )
    if resp.status >= 300, do: raise("ALPR API #{resp.status}")
    resp.body
  end

  # ── Notify backend after confirmed plate
  defp notify_backend(result) do
    Req.post!(
      String.replace(@api_base, "/api/v1", "/api/entry"),
      headers: [{"x-api-key", @api_key}],
      json: %{
        plate:      result["plate"],
        confidence: result["confidence"],
        country:    @country,
        timestamp:  DateTime.utc_now() |> DateTime.to_iso8601(),
        source:     "elixir-poller-v1",
      }
    )
  end

  # ── Main loop
  def run do
    IO.puts("[alpr] elixir poller started")
    IO.puts("[camera] #{@rtsp_url}")
    loop(%{last_plate_ms: 0, errors: 0})
  end

  defp loop(state) do
    t0 = System.monotonic_time(:millisecond)

    state =
      cond do
        System.monotonic_time(:millisecond) - state.last_plate_ms < @cooldown_ms ->
          IO.puts("[cooldown] skipping")
          :timer.sleep(@interval_ms)
          state

        true ->
          state = case capture_frame() do
            {:error, reason} ->
              errors = state.errors + 1
              IO.puts("[error] ##{errors}: #{reason}")
              if errors >= @max_retries do
                IO.puts("[error] too many failures, waiting 30s")
                :timer.sleep(30_000)
                %{state | errors: 0}
              else
                %{state | errors: errors}
              end

            {:ok, current, previous} ->
              state = %{state | errors: 0}
              if not detect_motion(current, previous) do
                IO.puts("[motion] none")
                state
              else
                IO.puts("[motion] detected — scanning plate")
                try do
                  result = scan_plate()
                  IO.puts("[alpr] #{inspect(result)}")
                  plate = result["plate"]
                  if plate && plate != "" do
                    IO.puts("[plate] #{plate} (#{result["confidence"]}%)")
                    notify_backend(result)
                    IO.puts("[cooldown] #{@cooldown_ms / 1000}s")
                    %{state | last_plate_ms: System.monotonic_time(:millisecond)}
                  else
                    IO.puts("[plate] none found")
                    state
                  end
                rescue
                  e -> IO.puts("[error] #{Exception.message(e)}"); state
                end
              end
          end

          elapsed = System.monotonic_time(:millisecond) - t0
          sleep   = max(0, @interval_ms - elapsed)
          :timer.sleep(sleep)
          state
      end

    loop(state)
  end
end

Poller.run()
