document.addEventListener("DOMContentLoaded", function () {
    const map = new maplibregl.Map({
        container: "map",
        style: "https://tiles.openfreemap.org/styles/liberty",
        center: [10, 50],
        zoom: 4,
        pitch: 25,
    });

    map.addControl(new maplibregl.NavigationControl({
        visualizePitch: true,
    }));
    map.addControl(new maplibregl.TerrainControl({
        source: "terrain",
        exaggeration: 1.5,
    }));

    const markers = [];

    map.on("load", function () {
        // 3D terrain
        map.addSource("terrain", {
            type: "raster-dem",
            tiles: ["https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png"],
            encoding: "terrarium",
            tileSize: 256,
            maxzoom: 15,
        });
        map.setTerrain({ source: "terrain", exaggeration: 1.5 });

        map.addSource("hillshade-source", {
            type: "raster-dem",
            tiles: ["https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png"],
            encoding: "terrarium",
            tileSize: 256,
            maxzoom: 15,
        });
        map.addLayer({
            id: "hillshade",
            type: "hillshade",
            source: "hillshade-source",
            paint: {
                "hillshade-shadow-color": "#473B24",
                "hillshade-illumination-anchor": "map",
                "hillshade-exaggeration": 0.5,
            },
        }, "building");
        fetch("/t/" + shareToken + "/track")
            .then(function (r) {
                return r.json();
            })
            .then(function (geojson) {
                // Add track line source
                map.addSource("track", {
                    type: "geojson",
                    data: {
                        type: "FeatureCollection",
                        features: geojson.features.filter(function (f) {
                            return f.properties.type === "track";
                        }),
                    },
                });

                map.addLayer({
                    id: "track-line",
                    type: "line",
                    source: "track",
                    paint: {
                        "line-color": "#b85c38",
                        "line-width": 3.5,
                    },
                });

                // Add entry markers
                var entryFeatures = geojson.features.filter(function (f) {
                    return f.properties.type === "entry";
                });

                entryFeatures.forEach(function (f) {
                    var el = document.createElement("div");
                    if (f.properties.photo) {
                        el.className = "map-marker-photo";
                        var img = document.createElement("img");
                        img.src = "/uploads/" + f.properties.photo;
                        el.appendChild(img);
                    } else {
                        el.className = "map-marker";
                    }

                    var marker = new maplibregl.Marker({ element: el })
                        .setLngLat(f.geometry.coordinates)
                        .addTo(map);

                    // Click marker -> scroll to entry
                    el.addEventListener("click", function () {
                        var entryEl = document.querySelector(
                            '[data-entry-id="' + f.properties.id + '"]'
                        );
                        if (entryEl) {
                            entryEl.scrollIntoView({
                                behavior: "smooth",
                                block: "center",
                            });
                            highlightEntry(entryEl);
                        }
                    });

                    markers.push({
                        marker: marker,
                        entryId: f.properties.id,
                    });
                });

                // Current position marker (last point of the track)
                var trackFeature = geojson.features.find(function (f) {
                    return f.properties.type === "track";
                });
                var currentPosMarker = null;
                if (trackFeature && trackFeature.geometry.coordinates.length > 0) {
                    var lastCoord = trackFeature.geometry.coordinates[trackFeature.geometry.coordinates.length - 1];
                    var el = document.createElement("div");
                    el.className = "map-marker-current";
                    el.innerHTML = '<svg width="36" height="48" viewBox="0 0 36 48" fill="none" xmlns="http://www.w3.org/2000/svg">' +
                        '<path d="M18 0C8.06 0 0 8.06 0 18c0 12.6 18 30 18 30s18-17.4 18-30C36 8.06 27.94 0 18 0z" fill="#b85c38"/>' +
                        '<circle cx="18" cy="18" r="8" fill="white"/>' +
                        '<circle cx="18" cy="18" r="4" fill="#b85c38"/>' +
                        '</svg>';
                    currentPosMarker = new maplibregl.Marker({ element: el, anchor: "bottom" })
                        .setLngLat(lastCoord)
                        .addTo(map);
                }

                // Fit bounds to track + markers
                fitMapBounds(geojson);

                // SSE live updates
                var evtSource = new EventSource("/t/" + shareToken + "/sse");

                evtSource.addEventListener("trackpoint", function (e) {
                    var point = JSON.parse(e.data);
                    var trackSource = map.getSource("track");
                    if (trackSource) {
                        var data = trackSource._data || trackSource.serialize().data;
                        if (data && data.features && data.features.length > 0) {
                            data.features[0].geometry.coordinates.push([point.lon, point.lat]);
                            trackSource.setData(data);
                        }
                    }
                    // Move current position marker
                    if (currentPosMarker) {
                        currentPosMarker.setLngLat([point.lon, point.lat]);
                    }
                });

                evtSource.addEventListener("entry", function () {
                    // Reload the page to show the new entry
                    window.location.reload();
                });
            });
    });

    // Scroll timeline -> pan map (center-based tracking)
    var timelineEl = document.getElementById("timeline");
    var allEntries = Array.from(document.querySelectorAll(".timeline-entry[data-lat]"));
    var activeEntryId = null;

    function updateActiveEntry() {
        var timelineRect = timelineEl.getBoundingClientRect();
        var centerY = timelineRect.top + timelineRect.height * 0.2;

        var closest = null;
        var closestDist = Infinity;

        allEntries.forEach(function (el) {
            var dot = el.querySelector(".timeline-dot");
            if (!dot) return;
            var dotRect = dot.getBoundingClientRect();
            var dotCenter = dotRect.top + dotRect.height / 2;
            var dist = Math.abs(dotCenter - centerY);
            if (dist < closestDist) {
                closestDist = dist;
                closest = el;
            }
        });

        if (!closest) return;
        var entryId = closest.dataset.entryId;
        if (entryId === activeEntryId) return;
        activeEntryId = entryId;

        var lat = parseFloat(closest.dataset.lat);
        var lon = parseFloat(closest.dataset.lon);
        if (!isNaN(lat) && !isNaN(lon)) {
            map.flyTo({
                center: [lon, lat],
                zoom: Math.max(map.getZoom(), 10),
                duration: 500,
            });
        }

        highlightEntry(closest);

        markers.forEach(function (m) {
            var el = m.marker.getElement();
            if (String(m.entryId) === String(entryId)) {
                el.classList.add("map-marker-active");
            } else {
                el.classList.remove("map-marker-active");
            }
        });
    }

    var scrollTimeout;
    timelineEl.addEventListener("scroll", function () {
        clearTimeout(scrollTimeout);
        scrollTimeout = setTimeout(updateActiveEntry, 80);
    });
    updateActiveEntry();

    // Stop timeline line at the current entry's dot
    var currentEntry = document.querySelector(".timeline-current");
    if (currentEntry) {
        var line = currentEntry.closest(".timeline-line");
        if (line) {
            var dot = currentEntry.querySelector(".timeline-dot");
            var dotBottom = dot.getBoundingClientRect().bottom;
            var lineBottom = line.getBoundingClientRect().bottom;
            line.style.setProperty("--current-height", (lineBottom - dotBottom) + "px");
        }
    }

    // Click timeline dots or dates -> fly to location on map
    document.querySelectorAll(".timeline-dot, .timeline-date").forEach(function (el) {
        el.style.cursor = "pointer";
        el.addEventListener("click", function () {
            var entry = el.closest(".timeline-entry");
            if (!entry) return;
            var lat = parseFloat(entry.dataset.lat);
            var lon = parseFloat(entry.dataset.lon);
            if (!isNaN(lat) && !isNaN(lon)) {
                map.flyTo({
                    center: [lon, lat],
                    zoom: Math.max(map.getZoom(), 12),
                    duration: 800,
                });
                highlightEntry(entry);
                var entryId = entry.dataset.entryId;
                markers.forEach(function (m) {
                    var mel = m.marker.getElement();
                    if (String(m.entryId) === String(entryId)) {
                        mel.classList.add("map-marker-active");
                    } else {
                        mel.classList.remove("map-marker-active");
                    }
                });
            }
        });
    });

    // Position distance labels at the midpoint between consecutive dots
    positionDistanceLabels();

    function positionDistanceLabels() {
        var dots = document.querySelectorAll(".timeline-dot");
        var labels = document.querySelectorAll(".timeline-distance-label");
        var entries = document.querySelectorAll(".timeline-entry");

        var labelIdx = 0;
        for (var i = 0; i < entries.length; i++) {
            if (!entries[i].classList.contains("has-distance")) continue;
            var label = labels[labelIdx];
            if (!label || i === 0) { labelIdx++; continue; }

            // Find the previous dot and current dot positions (relative to timeline)
            var prevDot = entries[i - 1].querySelector(".timeline-dot");
            var curDot = entries[i].querySelector(".timeline-dot");
            var prevRect = prevDot.getBoundingClientRect();
            var curRect = curDot.getBoundingClientRect();

            // Midpoint between the two dots
            var midY = (prevRect.top + prevRect.bottom + curRect.top + curRect.bottom) / 4;

            // Position label relative to its parent entry
            var entryRect = entries[i].getBoundingClientRect();
            label.style.top = (midY - entryRect.top) + "px";

            labelIdx++;
        }
    }

    // Lightbox
    var lightbox = document.createElement("div");
    lightbox.id = "lightbox";
    lightbox.innerHTML =
        '<div class="lightbox-backdrop"></div>' +
        '<div class="lightbox-media"></div>' +
        '<button class="lightbox-prev">&lsaquo;</button>' +
        '<button class="lightbox-next">&rsaquo;</button>' +
        '<button class="lightbox-close">&times;</button>' +
        '<span class="lightbox-counter"></span>';
    document.body.appendChild(lightbox);

    var lightboxMedia = lightbox.querySelector(".lightbox-media");
    var lightboxCounter = lightbox.querySelector(".lightbox-counter");
    var currentMedia = [];
    var currentIndex = 0;

    function openLightbox(el) {
        var container = el.closest(".timeline-photos");
        if (!container) return;
        currentMedia = Array.from(container.querySelectorAll("img, video"));
        currentIndex = currentMedia.indexOf(el);
        if (currentIndex === -1) currentIndex = 0;
        showMedia();
        lightbox.classList.add("active");
    }

    function showMedia() {
        var src = currentMedia[currentIndex].src;
        var isVideo = currentMedia[currentIndex].tagName === "VIDEO";
        if (isVideo) {
            lightboxMedia.innerHTML = '<video class="lightbox-img" src="' + src + '" controls autoplay></video>';
        } else {
            lightboxMedia.innerHTML = '<img class="lightbox-img" src="' + src + '">';
        }
        lightboxCounter.textContent = (currentIndex + 1) + " / " + currentMedia.length;
    }

    function closeLightbox() {
        lightbox.classList.remove("active");
        lightboxMedia.innerHTML = "";
    }

    lightbox.querySelector(".lightbox-backdrop").addEventListener("click", closeLightbox);
    lightbox.querySelector(".lightbox-close").addEventListener("click", closeLightbox);

    lightbox.querySelector(".lightbox-prev").addEventListener("click", function (e) {
        e.stopPropagation();
        currentIndex = (currentIndex - 1 + currentMedia.length) % currentMedia.length;
        showMedia();
    });

    lightbox.querySelector(".lightbox-next").addEventListener("click", function (e) {
        e.stopPropagation();
        currentIndex = (currentIndex + 1) % currentMedia.length;
        showMedia();
    });

    document.addEventListener("keydown", function (e) {
        if (!lightbox.classList.contains("active")) return;
        if (e.key === "Escape") closeLightbox();
        if (e.key === "ArrowLeft") {
            currentIndex = (currentIndex - 1 + currentMedia.length) % currentMedia.length;
            showMedia();
        }
        if (e.key === "ArrowRight") {
            currentIndex = (currentIndex + 1) % currentMedia.length;
            showMedia();
        }
    });

    document.querySelectorAll(".timeline-photos img").forEach(function (el) {
        el.style.cursor = "pointer";
        el.addEventListener("click", function () {
            openLightbox(el);
        });
    });

    document.querySelectorAll(".timeline-photos .video-wrap").forEach(function (wrap) {
        var video = wrap.querySelector("video");
        wrap.style.cursor = "pointer";
        wrap.addEventListener("click", function () {
            openLightbox(video);
        });
    });

    // Photo scroll arrows
    document.querySelectorAll(".timeline-photos-wrap").forEach(function (wrap) {
        var strip = wrap.querySelector(".timeline-photos");
        var left = wrap.querySelector(".scroll-arrow-left");
        var right = wrap.querySelector(".scroll-arrow-right");

        function updateArrows() {
            var canScrollLeft = strip.scrollLeft > 5;
            var canScrollRight = strip.scrollLeft < strip.scrollWidth - strip.clientWidth - 5;
            left.classList.toggle("visible", canScrollLeft);
            right.classList.toggle("visible", canScrollRight);
        }

        strip.addEventListener("scroll", updateArrows);
        updateArrows();

        left.addEventListener("click", function () {
            strip.scrollLeft -= strip.clientWidth * 0.7;
        });
        right.addEventListener("click", function () {
            strip.scrollLeft += strip.clientWidth * 0.7;
        });
    });

    function highlightEntry(el) {
        document
            .querySelectorAll(".timeline-entry.active")
            .forEach(function (e) {
                e.classList.remove("active");
            });
        el.classList.add("active");
    }

    function fitMapBounds(geojson) {
        var bounds = null;

        geojson.features.forEach(function (f) {
            if (f.geometry.type === "LineString") {
                f.geometry.coordinates.forEach(function (c) {
                    if (!bounds) {
                        bounds = new maplibregl.LngLatBounds(c, c);
                    } else {
                        bounds.extend(c);
                    }
                });
            } else if (f.geometry.type === "Point") {
                var c = f.geometry.coordinates;
                if (!bounds) {
                    bounds = new maplibregl.LngLatBounds(c, c);
                } else {
                    bounds.extend(c);
                }
            }
        });

        if (bounds) {
            map.fitBounds(bounds, { padding: 50 });
        }
    }
});
