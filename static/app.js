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
                        "line-color": "#2563eb",
                        "line-width": 3,
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
                });

                evtSource.addEventListener("entry", function () {
                    // Reload the page to show the new entry
                    window.location.reload();
                });
            });
    });

    // Scroll timeline -> pan map
    var observer = new IntersectionObserver(
        function (observerEntries) {
            observerEntries.forEach(function (oe) {
                if (oe.isIntersecting) {
                    var entryId = oe.target.dataset.entryId;
                    var lat = parseFloat(oe.target.dataset.lat);
                    var lon = parseFloat(oe.target.dataset.lon);
                    if (!isNaN(lat) && !isNaN(lon)) {
                        map.flyTo({
                            center: [lon, lat],
                            zoom: Math.max(map.getZoom(), 10),
                            duration: 500,
                        });
                    }
                    highlightEntry(oe.target);

                    // Highlight active map marker
                    markers.forEach(function (m) {
                        var el = m.marker.getElement();
                        if (String(m.entryId) === String(entryId)) {
                            el.classList.add("map-marker-active");
                        } else {
                            el.classList.remove("map-marker-active");
                        }
                    });
                }
            });
        },
        {
            root: document.getElementById("timeline"),
            threshold: 0.5,
        }
    );

    document.querySelectorAll(".timeline-entry[data-lat]").forEach(function (el) {
        observer.observe(el);
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
