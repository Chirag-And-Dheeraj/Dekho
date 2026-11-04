const eventSource = new EventSource(`${window.ENV.API_URL}/server-events/`, {
  withCredentials: true,
});
const toaster = new Toast();
const handledEventIDs = new Set();

eventSource.onopen = () => {
  console.log("SSE connected");
};

eventSource.onerror = (error) => {
  console.error("SSE Error:", error);
};

// TODO: handle case when event comes when the element is not in the DOM
eventSource.addEventListener("video_status", (event) => {
  try {
    const data = JSON.parse(event.data);
    if (event.lastEventId) {
      if (handledEventIDs.has(event.lastEventId)) return;
      handledEventIDs.add(event.lastEventId);
      if (handledEventIDs.size > 256) {
        handledEventIDs.delete(handledEventIDs.values().next().value);
      }
    }
    console.log("Update event received:", data);

    const { id: videoId, status: videoStatus, title: videoTitle = "Unknown Video", thumbnail } = data;

    // First, get the video-container element
    const videoContainer = document.querySelector("video-container");

    if (videoContainer && videoContainer.shadowRoot) {
      // Then, query for the specific video-item within its shadowRoot
      const videoItemElement = videoContainer.shadowRoot.querySelector(
        `video-item[video-id="${videoId}"]`
      );

      if (videoItemElement) {
        // Update the 'status' attribute of the video-item.
        // This will trigger the attributeChangedCallback in videoItem.js,
        // which then updates the UI to show the correct processing state (loader, failed, or completed).
        videoItemElement.setAttribute("status", videoStatus);
        videoItemElement.setAttribute("thumbnail", thumbnail);

        // Show toast messages based on the status
        if (videoStatus === 2) {
          // ProcessingCompleted
          toaster.success(videoTitle, "Processing completed.");
        } else if (videoStatus === -1) {
          // ProcessingFailed
          toaster.error(videoTitle, "Processing failed.");
        }
      } else {
        console.warn(
          `Video item with ID ${videoId} not found in the video-container's shadow DOM.`
        );
      }
    } else {
      console.warn("video-container element or its shadowRoot not found in the DOM.");
    }
  } catch (e) {
    console.error("Error parsing SSE data or updating UI:", e);
  }
});
