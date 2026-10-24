const fileForm = document.getElementById("file-form");
const divOutput = document.getElementById("divOutput");
const video = document.getElementById("video");
const titleElement = document.getElementById("title");
const descriptionElement = document.getElementById("description");
const fileError = document.getElementById("fileError");
const titleError = document.getElementById("titleError");
const descriptionError = document.getElementById("descriptionError");
const uploadVideoButton = document.getElementById("uploadVideoButton");
const progressContainer = document.getElementById("progressContainer");
const progressBar = document.getElementById("progressBar");

let uploadInProgress = false;
let uploadInProgressVideoID = null;

function handleBeforeUnload(event) {
  if (!uploadInProgress) return;
  event.preventDefault();
  event.returnValue = "Upload is in progress. Are you sure you want to leave?";
}

function checkFileType(file) {
  const supportedTypes = JSON.parse(localStorage.getItem("SUPPORTED_FILE_TYPES") || "[]");
  if (!supportedTypes.some((type) => type.file_type === file.type)) {
    const extensions = supportedTypes.map((type) => type.file_extension).join(", ");
    fileError.textContent = `Only ${extensions} files are supported`;
    fileError.style.display = "block";
    return false;
  }
  return true;
}

async function responseError(response, fallback) {
  try {
    const body = await response.json();
    if (body.error) return body.error;
  } catch (_) {
    // Use a status-based message if the response isn't JSON.
  }
  return `${fallback} (HTTP ${response.status})`;
}

function uploadPartToStorage(url, part, onProgress) {
  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open("PUT", url);
    request.upload.addEventListener("progress", (event) => {
      if (event.lengthComputable) onProgress(event.loaded);
    });
    request.addEventListener("load", () => {
      if (request.status >= 200 && request.status < 300) {
        const etag = request.getResponseHeader("ETag");
        if (etag) resolve(etag);
        else reject(new Error("File upload failed. Check the file and its size, then try again later."));
      }
      else if (request.status === 413) reject(new Error("File too large."));
      else reject(new Error("File upload failed. Check the file and its size, then try again later."));
    });
    request.addEventListener("error", () => reject(new Error("File upload failed. Check the file and its size, then try again later.")));
    request.addEventListener("abort", () => reject(new Error("Upload was cancelled.")));
    request.send(part);
  });
}

async function uploadMultipart(videoID, file, partSize, parts) {
  const maxParallelParts = 5;
  const loadedByPart = new Array(parts.length).fill(0);
  const uploadedParts = [];

  for (let offset = 0; offset < parts.length; offset += maxParallelParts) {
    const batch = parts.slice(offset, offset + maxParallelParts);
    const results = await Promise.allSettled(batch.map(async ({ part_number: partNumber, upload_url: uploadURL }) => {
      const start = (partNumber - 1) * partSize;
      const part = file.slice(start, Math.min(start + partSize, file.size));
      const etag = await uploadPartToStorage(uploadURL, part, (loaded) => {
        loadedByPart[partNumber - 1] = loaded;
        const uploadedBytes = loadedByPart.reduce((total, bytes) => total + bytes, 0);
        const percentage = Math.min(Math.round((uploadedBytes / file.size) * 100), 99);
        progressBar.style.width = `${percentage}%`;
        divOutput.textContent = `${percentage}%`;
      });
      loadedByPart[partNumber - 1] = part.size;
      return { part_number: partNumber, etag };
    }));
    const failedUpload = results.find((result) => result.status === "rejected");
    if (failedUpload) throw failedUpload.reason;
    uploadedParts.push(...results.map((result) => result.value));
  }

  uploadedParts.sort((a, b) => a.part_number - b.part_number);

  divOutput.textContent = "99%";
  const completeResponse = await fetch(`${window.ENV.API_URL}/video/${encodeURIComponent(videoID)}/complete`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ parts: uploadedParts }),
  });
  if (!completeResponse.ok) throw new Error(await responseError(completeResponse, "Unable to complete upload"));
}

fileForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const title = titleElement.value.trim();
  const description = descriptionElement.value.trim();
  const file = video.files[0];
  const regex = /^[a-zA-Z0-9\s\-_',.!&():]+$/;

  if (!regex.test(title)) { titleError.textContent = "Invalid Title"; titleError.style.display = "block"; return; }
  if (!regex.test(description)) { descriptionError.textContent = "Invalid Description"; descriptionError.style.display = "block"; return; }
  if (!file) { fileError.textContent = "Please select a file"; fileError.style.display = "block"; return; }
  if (!checkFileType(file)) return;
  const sizeLimit = Number(localStorage.getItem("FILE_SIZE_LIMIT"));
  if (sizeLimit && file.size > sizeLimit) {
    fileError.textContent = "File too large.";
    fileError.style.display = "block";
    return;
  }

  titleError.style.display = "none";
  descriptionError.style.display = "none";
  fileError.style.display = "none";
  progressContainer.style.display = "block";
  progressBar.style.width = "0%";
  divOutput.textContent = "Preparing upload...";
  uploadInProgress = true;
  window.addEventListener("beforeunload", handleBeforeUnload);
  uploadVideoButton.disabled = true;

  try {
    const createResponse = await fetch(`${window.ENV.API_URL}/video/`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title, description, file_name: file.name, file_size: file.size, content_type: file.type }),
    });
    if (!createResponse.ok) {
      const message = await responseError(createResponse, "Unable to start upload");
      throw new Error(/size exceeds|too large/i.test(message) ? "File too large." : message);
    }
    const { video_id: videoID, part_size: partSize, parts } = await createResponse.json();
    uploadInProgressVideoID = videoID;
    await uploadMultipart(videoID, file, partSize, parts);

    progressBar.style.width = "100%";
    divOutput.textContent = "Upload complete! Your video is processing. Redirecting to your videos...";
    window.setTimeout(() => { window.location.href = "/list"; }, 2500);
  } catch (error) {
    if (uploadInProgressVideoID) {
      try {
        await fetch(`${window.ENV.API_URL}/video/${encodeURIComponent(uploadInProgressVideoID)}/abort`, { method: "POST" });
      } catch (abortError) {
        console.warn("Unable to clean up incomplete upload", abortError);
      }
    }
    divOutput.textContent = error.message || "Upload failed. Please try again.";
    uploadVideoButton.disabled = false;
  } finally {
    uploadInProgressVideoID = null;
    uploadInProgress = false;
    window.removeEventListener("beforeunload", handleBeforeUnload);
  }
});
