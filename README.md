# Dekho: An on-demand video streaming server

## Some Achievements ✨

We launched [Dekho](https://peerlist.io/dheerajlalwani/project/dekho) on [Peerlist](https://peerlist.io) in the 3rd Week of January 2025 and we had a pretty good response.

![Project of the week](/documentation/static/Winner-Medal-Weekly.svg) ![Project of the week](/documentation/static/Winner-Medal-Monthly.svg)

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=Chirag-and-Dheeraj/video-streaming-server&type=Timeline)](https://www.star-history.com/#Chirag-and-Dheeraj/video-streaming-server&Timeline)

## Overview

Dekho is a research and study project aimed at understanding and learning about audio and video streaming. Our primary focus was on implementing the [HLS (HTTP Live Streaming)](https://developer.apple.com/streaming) protocol to build an on-demand video streaming server.

## Setup Instructions

### Basic Steps

- Clone the repository
- Create an **_[Appwrite storage bucket](https://appwrite.io/docs/products/storage)_**
  - Make sure that you make a note of `APPWRITE_KEY`, `APPWRITE_PROJECT_ID` and the `BUCKET_ID`.
  - Set `APPWRITE_S3_ENDPOINT` to the project's regional S3 endpoint (for example, `https://fra.cloud.appwrite.io/v1/s3`) and `APPWRITE_S3_REGION` to `auto` or the Appwrite region code. The API key needs Storage read and write scopes.

### With Docker

- Install `docker-compose` on your machine.
- cd into the root of the repository.
  - Create a `.env` file using **_[.env.docker](.env.docker)_** as a reference.
- Just run `docker-compose up` to start the server.

### Without Docker

- Install the `make` utility because we have created a **_[Makefile](Makefile)_** to ease the setup
- Now we are moving to the dependency installation steps.
- This project needs `go version go1.23`, `ffmpeg` utility and `psql (PostgreSQL) 16.3` database.
- To install these, and the Go dependencies, run `make install-dev`.
  - The `make install-dev` takes care of installing PostgreSQL, Go, FFMpeg, golang-migrate and Go dependencies.
  - Run `make start-postgres` to start the postgres service.
  - Create a database user by running this command `sudo -u postgres createuser -s username_here -P`
  - `-P` will prompt for a password.
  - Enter the `psql` shell by running this command `sudo -u postgres psql` and create a database by running `CREATE DATABASE <database_name>;`.
  - Create a `.env` file using **_[.env.local](.env.local)_** as a reference.
- Once the dependencies are installed successfully, run `make cleanstart`.
- This command will create all the necessary folders and start the server on `http://127.0.0.1:8000`
- If you just want to run the server, run: `make start`
- If you just want to clean up, run: `make clean`

## Technologies Used

- **Server:** Go
- **Database:** PostgreSQL
- **Storage:** [Appwrite Storage](https://appwrite.io/docs/products/storage)
- **Video Processing:** [FFMPEG](https://ffmpeg.org) for breaking down videos into .ts chunks
- **Video Player:** [HLS.js](https://github.com/video-dev/hls.js)
- **Frontend:** HTML, CSS, JS

## Project Architecture

Appwrite S3 object keys use `raw_files/<videoID><extension>`, `hls_chunks/<videoID>/<chunk>.ts`, `manifests/<videoID>.m3u8`, and `thumbnails/<videoID>.png`.

![Architecture Diagram](documentation/static/dekho-architecture.excalidraw.png)

## Key Resources

Our journey has been greatly enriched by the insights and guidance from various resources. A pivotal article that set us on the right path is "[Learning the basics of video streaming with Golang](https://www.rohitmundra.com/video-streaming-server)" by [Rohit Mundra](https://twitter.com/brohit3).

For a comprehensive list of resources that have been instrumental in our learning and development process, please refer to our [documentation](https://github.com/Chirag-And-Dheeraj/video-streaming-server/blob/main/documentation/video-streaming-project-stuff/links.md).

## Getting Involved

We welcome any queries or contributions to the project. If you have any questions or suggestions, please feel free to reach out to us:

- **Dheeraj Lalwani:** [Twitter](https://twitter.com/DhiruCodes)
- **Chirag Lulla:** [Twitter](https://twitter.com/_chiraglulla_)

## Updates and Blog Posts

Stay tuned for upcoming blog posts and updates on our progress. If you haven't heard from us in a while, feel free to bug us about it!
