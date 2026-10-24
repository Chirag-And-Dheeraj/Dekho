class VideoItem extends HTMLElement {
  constructor() {
    super();
    this.shadow = this.attachShadow({ mode: "open" });
    const style = document.createElement("style");

    style.textContent = `
          :host(.list-mode-item) .video-item {
              display: flex;
              align-items: center;
              justify-content: space-between;
              width: 100%;
              background: #262626;
              margin: 6px;
              padding: 12px;
              border-radius: 12px;
          }
           :host(.list-mode-item) .thumbnail-container {
              width: 200px;
              height: 120px;
              flex-shrink: 0;
              border-radius: 12px;
          }
          :host(.list-mode-item) .content {
              flex-grow: 1;
              text-align: left;
              padding: 0 1rem;
              overflow: hidden;
          }
          :host(.list-mode-item) .actions {
              margin-left: auto;
              padding: 1em;
              flex-shrink: 0;
          }

          :host(.grid-mode-item) .video-item {
              display: flex;
              flex-direction: column;
              align-items: stretch;
              height: 100%;
              overflow: hidden;
              background-color: #262626;
              border-radius: 8px;
          }
          :host(.grid-mode-item) .thumbnail-container {
              width: 100%;
              aspect-ratio: 16 / 9;
              height: auto;
              width: auto;
              flex-shrink: 0;
              border-radius: 8px 8px 0 0;
          }
           :host(.grid-mode-item) .thumbnail-container img {
                object-fit: cover;
                border-radius: 8px 8px 0 0;
           }
          :host(.grid-mode-item) .content {
              padding: 0.75rem 0.75rem 0.5rem 0.75rem;
              overflow: hidden;
              flex-grow: 1;
              display: flex;
              flex-direction: column;
          }
           :host(.grid-mode-item) .content .name {
               white-space: nowrap;
               overflow: hidden;
               text-overflow: ellipsis;
               margin-bottom: 0.25rem;
               font-size: 1.0em;
           }
           :host(.grid-mode-item) .content .description {
               font-size: 0.85em;
               color: #ccc;
               display: -webkit-box;
               -webkit-line-clamp: 2;
               -webkit-box-orient: vertical;
               overflow: hidden;
               text-overflow: ellipsis;
               line-height: 1.3;
               margin-bottom: 0.5rem;
               flex-grow: 1;
           }
          :host(.grid-mode-item) .actions {
              padding: 0rem 0.75rem 0.75rem 0.75rem;
              display: flex;
              justify-content: flex-end;
              margin-top: auto;
              flex-shrink: 0;
          }
           :host(.grid-mode-item) .action-button {
               padding: 0.4rem 0.6rem;
               font-size: 0.6em;
           }

          .video-item {
              color: white;
              cursor: pointer;
              position: relative;
              transition: transform 0.2s ease-in-out, box-shadow 0.2s ease-in-out;
          }
          .video-item:hover {
               transform: translateY(-3px);
               box-shadow: 0 5px 15px rgba(0,0,0,0.4);
          }

          .thumbnail-container {
              position: relative;
              overflow: hidden;
              background-color: #1a1a1a;
          }

          .thumbnail-container img {
              display: block;
              width: 100%;
              height: 100%;
          }

          .content .name {
              margin: 0 0 0.25rem 0;
              font-size: 1.1em;
              font-weight: 600;
              color: #ffffff;
          }
          .content .description {
              margin: 0;
              font-size: 0.9em;
              color: #b3b3b3;
              line-height: 1.4;
          }

          .actions {
              display: flex;
              gap: 0.5rem;
          }

          .action-button {
              padding: 0.5rem 0.75rem;
              border-radius: 4px;
              text-decoration: none;
              color: white;
              font-weight: 500;
              cursor: pointer;
              border: none;
              background-color: #3f3f3f;
              transition: background-color 0.2s ease;
          }
           .action-button:hover {
                background-color: #555;
           }

          .action-button.delete-modal,
          .action-button.delete {
              background-color: #d9534f;
              border-color: #d43f3a;
          }
          .action-button.delete-modal:hover,
          .action-button.delete:hover {
              background-color: #c9302c;
              border-color: #ac2925;
          }

          .delete-modal, .delete {
              background-color: #ff4444;
          }

          .update-modal, .update {
              background-color: #a0a0ff;;
          }

          .thumbnail-container {
              position: relative; /* Added for overlay positioning */
              width: 200px;
              height: 150px;
              overflow: hidden;
          }

          .thumbnail-container img {
              width: 100%;
              height: 100%;
              object-fit: content;
          }

          .video-item .thumbnail-container::before {
              content: '';
              position: absolute;
              top: 0;
              left: 0;
              width: 100%;
              height: 100%;
              background-color: rgba(0, 0, 0, 0);
              transition: background-color 0.3s ease;
              pointer-events: none;
              border-radius: inherit;
          }
          .video-item:hover .thumbnail-container::before {
              background-color: rgba(0, 0, 0, 0.4);
          }

          .play-button {
              position: absolute;
              top: 50%;
              left: 50%;
              transform: translate(-50%, -50%) scale(0.7);
              opacity: 0;
              transition: all 0.3s cubic-bezier(0.175, 0.885, 0.32, 1.275);
              border: none;
              background: rgba(0, 0, 0, 0.6);
              border-radius: 50%;
              width: 55px;
              height: 55px;
              display: flex;
              align-items: center;
              justify-content: center;
              cursor: pointer;
              pointer-events: none;
              box-shadow: 0 2px 5px rgba(0,0,0,0.3);
          }
          .play-button::before {
              content: '▶';
              color: white;
              font-size: 22px;
              margin-left: 4px;
          }
          .video-item:hover .play-button {
              opacity: 1;
              transform: translate(-50%, -50%) scale(1);
          }

          #title {
            width: 400px;
            padding: 10px;
            border-radius: 5px;
            border: none;
            outline: none;
          }
          
          #description {
            width: 400px;
            height: 70px;
            padding: 10px;
            border-radius: 5px;
            border: none;
            outline: none;
          }

          .special-red {
            color: #ff8e8e;
          }

          .special-blue {
            color: #a0a0ff;
          }
          .modal {
              position: fixed;
              top: 0;
              left: 0;
              width: 100%;
              height: 100%;
              background-color: rgba(0, 0, 0, 0.75);
              z-index: 1000;
              display: flex;
              justify-content: center;
              align-items: center;
              opacity: 0;
              transition: opacity 0.2s ease-in-out;
              pointer-events: none;
          }
          .modal[style*="display: flex"] {
              opacity: 1;
              pointer-events: auto;
          }

          .modal-content {
              background-color: #2b2b2b;
              padding: 2rem;
              border-radius: 8px;
              text-align: left;
              color: #eee;
              max-width: 420px;
              width: 90%;
              box-shadow: 0 5px 15px rgba(0,0,0,0.5);
              transform: scale(0.95);
              transition: transform 0.2s ease-in-out;
          }
          .modal[style*="display: flex"] .modal-content {
              transform: scale(1);
          }

          .modal input, .modal textarea, .modal label {
              font-size: 1.125em;
              border-radius: 5px;
              outline: none;
              border: none;
              padding: 0.25em;
              margin: 0.125em;
              width: 100%;
          }

          .modal-actions {
              display: flex;
              justify-content: center;
              gap: 1rem;
              margin-top: 1.5rem;
          }
           .modal-content p {
               margin-bottom: 0.5rem;
               color: #ccc;
           }
           .modal-content .name {
                font-weight: bold;
                margin: 0.5rem 0 1rem 0;
                color: white;
                word-break: break-all;
                background-color: #3f3f3f;
                padding: 0.5rem;
                border-radius: 4px;
                text-align: center;
           }

           .action-button.cancel {
               background-color: #6c757d;
               border-color: #6c757d;
           }
            .action-button.cancel:hover {
               background-color: #5a6268;
               border-color: #545b62;
           }

          /* New styles for status display */
          .status-message {
            font-size: 0.9em;
            margin-top: 0.5rem;
            text-align: center;
            padding: 0.5rem;
            border-radius: 4px;
            white-space: nowrap; /* Prevent wrapping for the message */
            overflow: hidden;
            text-overflow: ellipsis;
          }

          .status-failed {
            color: #ff4444;
          }

          .status-processing {
            color: #ffcc00;
            display: flex;
            align-items: center;
            justify-content: center;
          }

          .loader {
            border: 3px solid #f3f3f3;
            border-top: 3px solid #ffcc00;
            border-radius: 50%;
            width: 14px;
            height: 14px;
            animation: spin 1s linear infinite;
            margin-left: 8px;
          }

          @keyframes spin {
            0% { transform: rotate(0deg); }
            100% { transform: rotate(360deg); }
          }
          /* Adjustments for status message in list and grid view */
          :host(.list-mode-item) .content .status-message {
            text-align: left;
            margin-left: 0;
          }

          :host(.grid-mode-item) .content .status-message {
            margin-top: auto; /* Push to bottom in grid view */
            margin-bottom: 0.25rem;
          }
      `;

    const template = document.createElement("template");

    template.innerHTML = `
      <div class="video-item">
          <div class="thumbnail-container">
              <img class="thumbnail" alt="thumbnail" loading="lazy"/>
              <button class="play-button" aria-label="Play video"></button>
          </div>
          <div class="content">
              <h3 class="name"></h3>
              <p class="description"></p>
              <div class="status-message" style="display: none;"></div>
          </div>
          <div class="actions">
              <button class="action-button update-modal" title="Update Video">Edit</button>
              <button class="action-button delete-modal" title="Delete Video">Delete</button>
          </div>

      </div>
    `;

    const modalTemplate = document.createElement("template");

    modalTemplate.innerHTML = `
      <div class="modal" id="deleteConfirmModal" style="display: none;">
          <div class="modal-content">
              <p>Are you sure you want to delete:</p>
              <h3 class="name"></h3>
              <div class="modal-actions">
                  <button class="action-button cancel">Cancel</button>
                  <button class="action-button delete">Delete</button>
              </div>
          </div>
      </div>
    `;

    const updateModalTemplate = document.createElement("template");
    updateModalTemplate.innerHTML = `
      <div class="modal" id="updateModal">
          <div class="modal-content">
              <section id="form-section">
                <form id="file-form">
                  <label for="title">Title</label>
                  <br />
                  <input
                    id="title"
                    type="text"
                    name="title"
                    required
                  />

                  <span id="titleError" class="special-red" style="display: none"></span>

                  <br />
                  <br />

                  <label class="block" for="description">Description</label>
                  <br />
                  <textarea
                    id="description"
                    type="text"
                    name="description"
                    required
                  ></textarea>

                  <span
                    id="descriptionError"
                    class="special-red"
                    style="display: none"
                  ></span>

                  <br />
                  <div class="modal-actions">
                    <button type="button" class="action-button cancel">Cancel</button>
                    <button type="submit" class="action-button update">Save</button>
                  </div>
                </form>
              </section>
          </div>
      </div>
  `;

    this.shadow.appendChild(style);
    this.shadow.appendChild(template.content.cloneNode(true));
    this.shadow.appendChild(updateModalTemplate.content.cloneNode(true));
    this.shadow.appendChild(modalTemplate.content.cloneNode(true));

    this.videoItemElement = this.shadow.querySelector(".video-item");
    this.modalElement = this.shadow.querySelector("#deleteConfirmModal");
    this.updateModalElement = this.shadow.querySelector("#updateModal");
    this.statusMessageElement = this.shadow.querySelector(".status-message"); // Get the status message element
    this.playButton = this.shadow.querySelector(".play-button"); // Get the play button

    this.initialize();
    this.initializeModal();
    this.initializeUpdateModal();
  }

  initialize() {
    this.videoItemElement.addEventListener("click", (e) => {
      if (e.target.closest(".actions")) {
        return;
      }
      e.preventDefault();
      this.handlePlay();
    });

    const deleteModalButton = this.shadow.querySelector(".delete-modal");
    if (deleteModalButton) {
      deleteModalButton.addEventListener("click", (e) => {
        e.stopPropagation();
        const fileName = this.getAttribute("name") || "this video";
        const fileNameElement = this.modalElement.querySelector(".name");
        fileNameElement.textContent = fileName;
        this.modalElement.style.display = "flex";
      });
    }

    const updateModalButton = this.shadow.querySelector(".update-modal");
    if (updateModalButton) {
      updateModalButton.addEventListener("click", (e) => {
        e.stopPropagation();
        const title = this.getAttribute("name");
        const fileNameElement = this.updateModalElement.querySelector("#title");
        fileNameElement.value = title;
        const description = this.getAttribute("description");
        const descriptionElement = this.updateModalElement.querySelector("#description");
        descriptionElement.value = description;
        this.updateModalElement.style.display = "flex";
      });
    }
  }

  initializeUpdateModal() {
    const cancelButton = this.updateModalElement.querySelector("#updateModal .cancel");
    const fileForm = this.updateModalElement.querySelector("#file-form");

    cancelButton.addEventListener("click", () => {
      this.updateModalElement.style.display = "none";
    });

    fileForm.addEventListener("submit", (e) => {
      e.preventDefault();
      this.handleUpdate();
    });

    this.updateModalElement.addEventListener("click", (e) => {
      if (e.target === this.updateModalElement) {
        this.updateModalElement.style.display = "none";
      }
    });
  }

  initializeModal() {
    const cancelButton = this.modalElement.querySelector(".cancel");
    const deleteConfirmButton = this.modalElement.querySelector(".delete");

    cancelButton.addEventListener("click", (e) => {
      e.stopPropagation();
      this.modalElement.style.display = "none";
    });

    deleteConfirmButton.addEventListener("click", (e) => {
      e.stopPropagation();
      this.modalElement.style.display = "none";
      this.handleDelete();
    });

    this.modalElement.addEventListener("click", (e) => {
      if (e.target === this.modalElement) {
        this.modalElement.style.display = "none";
      }
    });
  }

  handlePlay() {
    // Only allow playing if the video is not in a processing state
    const status = parseInt(this.getAttribute("status")); // Parse status as integer
    if (status === 1 || status === -1) {
      // 1 for UploadedOnServer/Processing, -1 for ProcessingFailed
      return; // Prevent playback if still processing or failed
    }

    const videoId = this.getAttribute("video-id");
    if (videoId) {
      try {
        const watchUrl = new URL(`${window.ENV.API_URL}/watch`);
        watchUrl.searchParams.set("v", videoId);
        window.location.href = watchUrl.toString();
      } catch (e) {
        console.error("Error creating watch URL:", e);
        window.location.href = `/watch?v=${videoId}`;
      }
    } else {
      console.error("Video ID attribute is missing, cannot play.");
    }
  }

  handleDelete() {
    const videoId = this.getAttribute("video-id");
    if (!videoId) {
      console.error("Video ID missing, cannot delete.");
      alert("Cannot delete video: ID is missing.");
      return;
    }

    const deleteButton = this.shadowRoot.querySelector(".delete-modal");
    const originalText = deleteButton.textContent;
    const originalTitle = deleteButton.title;
    deleteButton.textContent = "Deleting...";
    deleteButton.title = "Deleting...";
    deleteButton.disabled = true;

    fetch(`${window.ENV.API_URL}/video/${videoId}`, {
      method: "DELETE",
      headers: {
        Accept: "application/json",
      },
    })
      .then(async (response) => {
        if (response.ok) {
          this.remove();
          this.dispatchEvent(
            new CustomEvent("item-deleted", {
              bubbles: true,
              composed: true,
              detail: { id: videoId },
            })
          );
        } else {
          let errorMsg = `HTTP ${response.status} ${response.statusText}`;
          try {
            const errData = await response.json();
            errorMsg = errData.detail || errData.message || errorMsg;
          } catch (e) {}
          console.error("Error deleting video:", errorMsg);
          alert(`Failed to delete video: ${errorMsg}`);
          deleteButton.textContent = "Error";
          deleteButton.title = "Deletion failed";
          setTimeout(() => {
            deleteButton.textContent = originalText;
            deleteButton.title = originalTitle;
            deleteButton.disabled = false;
          }, 2500);
        }
      })
      .catch((error) => {
        console.error("Network error during delete:", error);
        alert(`Failed to delete video: Network error. ${error.message}`);
        deleteButton.textContent = "Error";
        deleteButton.title = "Deletion failed";
        setTimeout(() => {
          deleteButton.textContent = originalText;
          deleteButton.title = originalTitle;
          deleteButton.disabled = false;
        }, 2500);
      });
  }

  handleUpdate() {
    const videoId = this.getAttribute("video-id");
    const updateButton = this.shadowRoot.querySelector(".update");
    const titleElement = this.shadowRoot.getElementById("title");
    const descriptionElement = this.shadowRoot.getElementById("description");
    const titleError = this.shadowRoot.getElementById("titleError");
    const descriptionError = this.shadowRoot.getElementById("descriptionError");

    const title = titleElement.value;
    const description = descriptionElement.value;
    const regex = /^[a-zA-Z0-9\s\-_',.!&():]+$/;

    if (!regex.test(title)) {
      titleError.textContent = "Invalid Title";
      titleError.style.display = "block";
      return;
    }

    if (!regex.test(description)) {
      descriptionError.textContent = "Invalid Description";
      descriptionError.style.display = "block";
      return;
    }

    updateButton.textContent = "Saving...";

    const changes = {
      title,
      description,
    };

    fetch(`${window.ENV.API_URL}/video/${videoId}`, {
      method: "PATCH",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
      },
      body: JSON.stringify(changes),
    })
      .then((response) => {
        if (response.ok) {
          this.remove();
          window.location.reload(true);
        } else {
          updateButton.textContent = "Error";
        }
      })
      .catch((error) => {
        updateButton.textContent = "Error";
        console.error("Error updating video details:", error);
      });
  }

  static get observedAttributes() {
    return ["name", "description", "thumbnail", "video-id", "status"];
  }

  attributeChangedCallback(name, oldValue, newValue) {
    if (name === "thumbnail") {
      const element = this.shadow.querySelector(".thumbnail");
      if (element) {
        element.src = newValue && newValue.startsWith("/") ? `${window.ENV.API_URL}${newValue}` : newValue || "";
      }
    } else if (name === "name") {
      const element = this.shadow.querySelector(".content .name");
      if (element) {
        element.textContent = newValue || "Untitled Video";
      }
    } else if (name === "description") {
      const element = this.shadow.querySelector(".content .description");
      if (element) {
        element.textContent = newValue || "";
      }
    } else if (name === "status") {
      this.updateStatusDisplay(newValue);
    }
  }

  updateStatusDisplay(status) {
    this.statusMessageElement.style.display = "none";
    this.statusMessageElement.innerHTML = "";
    this.statusMessageElement.classList.remove("status-failed", "status-processing");
    this.playButton.style.display = "block";

    switch (parseInt(status)) {
      case -1:
        this.statusMessageElement.textContent = "Processing Failed";
        this.statusMessageElement.classList.add("status-failed");
        this.statusMessageElement.style.display = "block";
        this.playButton.style.display = "none";
        break;
      case 1:
        this.statusMessageElement.innerHTML = 'Processing <div class="loader"></div>';
        this.statusMessageElement.classList.add("status-processing");
        this.statusMessageElement.style.display = "flex";
        this.playButton.style.display = "none";
        break;
      case 0:
        this.statusMessageElement.textContent = "Upload Pending";
        this.statusMessageElement.style.display = "block";
        this.playButton.style.display = "none";
        break;
      case 2:
        break;
      default:
        break;
    }
  }
}

customElements.define("video-item", VideoItem);
