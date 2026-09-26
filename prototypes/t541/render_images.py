"""Rasterize the production-layout SVG previews without changing UI code."""
import pathlib
import subprocess
import tempfile
import xml.etree.ElementTree as ET

root = pathlib.Path(__file__).resolve().parent
for svg in sorted(root.glob("*.svg")):
    element = ET.parse(svg).getroot()
    width, height = int(element.attrib["width"]), int(element.attrib["height"])
    with tempfile.TemporaryDirectory(prefix="csquad-t541-render-") as temporary:
        capture = pathlib.Path(temporary) / "capture.png"
        profile = pathlib.Path(temporary) / "chrome"
        subprocess.run([
            "google-chrome", "--headless", "--no-sandbox", "--disable-gpu",
            "--hide-scrollbars", f"--user-data-dir={profile}",
            f"--window-size={width},{height + 100}", f"--screenshot={capture}",
            svg.as_uri(),
        ], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run([
            "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", str(capture),
            "-vf", f"crop={width}:{height}:0:0", "-frames:v", "1", str(svg.with_suffix(".png")),
        ], check=True)
    print(svg.with_suffix(".png").name)
