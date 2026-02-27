---
hide:
  - toc
  - navigation
---
<style>
.md-content__inner > h1:nth-child(1) {
    display: none;
}
.md-main__inner {
    height: 100%;
    margin-top: 0;
    padding-top: 0;
    max-width: 61rem;
}
.md-content__inner {
    margin: 0 auto;
    padding-top: 0;
}
.md-content__inner::before {
    height: 0;
}
.md-content__inner > p {
    margin-top: 0;
}
.md-footer {
    display: none;
}
.redoc-iframe {
    height: calc(100vh - 60px) !important;
    display: block;
    border: none;
}

</style>

<redoc src="../openapi.json"/>

<script>
document.addEventListener("DOMContentLoaded", function() {
    var iframe =
        document.querySelector(".redoc-iframe");
    if (!iframe) return;
    iframe.addEventListener("load", function() {
        try {
            var doc = iframe.contentDocument;
            if (!doc) return;
            var style = doc.createElement("style");
            style.textContent =
                "[data-role='redoc-description']" +
                " span," +
                ".redoc-wrap span[class^='sc-'] {" +
                "  color: inherit !important;" +
                "}";
            doc.head.appendChild(style);
        } catch (e) {}
    });
});
</script>
