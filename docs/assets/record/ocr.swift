// Privacy check helper: print the text macOS Vision finds in each image.
//   swiftc docs/assets/record/ocr.swift -o /tmp/ocr && /tmp/ocr frame-*.png
import Foundation
import Vision
import AppKit
for path in CommandLine.arguments.dropFirst() {
    guard let img = NSImage(contentsOfFile: path),
          let cg = img.cgImage(forProposedRect: nil, context: nil, hints: nil) else { continue }
    let req = VNRecognizeTextRequest()
    req.recognitionLevel = .accurate
    try? VNImageRequestHandler(cgImage: cg).perform([req])
    let lines = (req.results ?? []).compactMap { $0.topCandidates(1).first?.string }
    print("== \(path)")
    print(lines.joined(separator: "\n"))
}
