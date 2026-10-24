window.onload = async () => {
  let video = document.getElementById("video");
  const url = new URL(window.location.href);
  const searchParams = new URLSearchParams(url.search);
  const v = searchParams.get("v");
  const apiURL = window.ENV.API_URL.replace(/\/+$/, "");

  let response = await fetch(`${window.ENV.API_URL}/video/${v}`);
  let videoDetails = await response.json();

  let title = document.getElementById("video_title");
  let description = document.getElementById("video_description");

  titleText = document.createTextNode(videoDetails.title);
  descriptionText = document.createTextNode(videoDetails.description);

  title.appendChild(titleText);
  description.appendChild(descriptionText);

  if (Hls.isSupported()) {
    const manifestResponse = await fetch(`${apiURL}/video/${encodeURIComponent(v)}/stream/`);
    if (!manifestResponse.ok) throw new Error("Unable to load video manifest");
    const manifestText = await manifestResponse.text();
    const rewrittenManifest = manifestText
      .split(/\r?\n/)
      .map((line) => {
        const path = line.trim();
        if (!path || path.startsWith("#")) return line;

        const resolvedPath = new URL(path, manifestResponse.url).pathname;
        const segmentName = decodeURIComponent(resolvedPath.split("/").pop());
        return `${apiURL}/video/${encodeURIComponent(v)}/stream/${encodeURIComponent(segmentName)}`;
      })
      .join("\n");
    const manifestURL = URL.createObjectURL(
      new Blob([rewrittenManifest], { type: "application/vnd.apple.mpegurl" }),
    );

    var hls = new Hls();
    hls.loadSource(manifestURL);
    hls.attachMedia(video);
    hls.on(Hls.Events.MANIFEST_PARSED, function () {
      video.play();
    });
  } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
    video.src = `${window.ENV.API_URL}/video/${v}/stream/?inline=1`;
    video.addEventListener("loadedmetadata", function () {
      video.play();
    });
  }
};
