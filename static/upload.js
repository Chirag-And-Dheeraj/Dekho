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

function uploadToStorage(url, file, onProgress) {
  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open("PUT", url);
    request.setRequestHeader("Content-Type", file.type);
    request.upload.addEventListener("progress", (event) => {
      if (event.lengthComputable) onProgress(Math.round((event.loaded / event.total) * 100));
    });
    request.addEventListener("load", () => {
      if (request.status >= 200 && request.status < 300) resolve();
      else reject(new Error(`Storage upload failed (${request.status})`));
    });
    request.addEventListener("error", () => reject(new Error("Storage upload failed. Check your connection and try again.")));
    request.addEventListener("abort", () => reject(new Error("Upload was cancelled.")));
    request.send(file);
  });
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
    fileError.textContent = `File size is greater than ${sizeLimit / (1024 * 1024)} MB`;
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
    if (!createResponse.ok) throw new Error(await createResponse.text() || "Unable to start upload");
    const { video_id: videoID, upload_url: uploadURL } = await createResponse.json();

    divOutput.textContent = "Uploading directly to storage...";
    await uploadToStorage(uploadURL, file, (progress) => {
      progressBar.style.width = `${progress}%`;
      divOutput.textContent = `${progress}%`;
    });

    divOutput.textContent = "Confirming upload...";
    const completeResponse = await fetch(`${window.ENV.API_URL}/video/${videoID}/complete`, { method: "POST" });
    if (!completeResponse.ok) throw new Error(await completeResponse.text() || "Unable to confirm upload");

    progressBar.style.width = "100%";
    divOutput.textContent = "Upload complete! Your video is processing. Redirecting to your videos...";
    window.setTimeout(() => { window.location.href = "/list"; }, 2500);
  } catch (error) {
    divOutput.textContent = error.message || "Upload failed. Please try again.";
    uploadVideoButton.disabled = false;
  } finally {
    uploadInProgress = false;
    window.removeEventListener("beforeunload", handleBeforeUnload);
  }
});
