// Ganymede.app's executable, in two roles.
//
// With no arguments it is the launcher: Spotlight, a double-click or `open -a`
// runs it, it brings the harness up, and it exits. With --tile the Dashboard
// has spawned it as a child holding a pipe, and it is the Tile: the Dock tile
// and the menu-bar item, whose dropdown carries the Blocked/Ready/Working
// breakdown.
//
// The tile decides nothing. What the surfaces say arrives as one line of
// three counts on stdin — Blocked, Ready, Working — decided by internal/tile
// in Go where it can be tested. EOF means the Dashboard that was telling us
// has gone, and a count nobody is left to stand behind must not stay on
// screen — so the tile clears both surfaces and quits.
import AppKit

// repoKey is where `make launcher` writes this checkout's path, so the
// installed app always brings up the harness from the checkout it was built
// from.
let repoKey = "GanymedeRepo"

// blockedRed is session.Blocked.Colour() — the rail's own red for the one
// state this counts. The Dock badge keeps the system's red; this is for the
// menu bar, which has no badge of its own. readyGreen and workingBlue mirror
// session.Ready.Colour() and session.Working.Colour() the same way, so the
// dropdown's three rows read as the same vocabulary as the rail and the strip.
let blockedRed = NSColor(srgbRed: 0xF8 / 255, green: 0x51 / 255, blue: 0x49 / 255, alpha: 1)
let readyGreen = NSColor(srgbRed: 0x3F / 255, green: 0xB9 / 255, blue: 0x50 / 255, alpha: 1)
let workingBlue = NSColor(srgbRed: 0x58 / 255, green: 0xA6 / 255, blue: 0xFF / 255, alpha: 1)

// blockedGlyph, readyGlyph and workingGlyph are session.Blocked/.Ready/.Working's
// own Glyph(), and idleGlyph is the app's own mark: the count reads as the
// same fact the rail and the strip show, and the menu bar still says the
// harness is up when nothing is waiting on you.
let blockedGlyph = "█"
let readyGlyph = "●"
let workingGlyph = "⠿"
let idleGlyph = "◑"

let ghostty = URL(fileURLWithPath: "/Applications/Ghostty.app")

// forward brings Ghostty to the front — what clicking either surface is for.
// The Dashboard is a keystroke away once you are in the window, so neither
// surface tries to jump to a particular Session.
func forward() {
    NSWorkspace.shared.openApplication(at: ghostty, configuration: NSWorkspace.OpenConfiguration())
}

// complain is the only place either role says anything: its stderr is the
// terminal that launched it, or the Dashboard's own pane, so it says as little
// as it can.
func complain(_ what: String) {
    FileHandle.standardError.write((what + "\n").data(using: .utf8)!)
}

// launch is the launcher role: bring the harness up from the checkout this
// bundle was installed from, and exit when it has.
//
// The explicit PATH matters here in a way it would not in a terminal: a
// process launched by LaunchServices inherits the login session's environment
// rather than an interactive shell's, and `up` shells out to tmux and looks
// terminal-notifier up by bare name — both Homebrew-installed.
func launch() -> Never {
    guard let repo = Bundle.main.infoDictionary?[repoKey] as? String, !repo.hasPrefix("@@") else {
        complain("Ganymede.app does not know where its checkout is: run make launcher again")
        exit(1)
    }
    let process = Process()
    process.executableURL = URL(fileURLWithPath: repo + "/bin/ganymede")
    process.arguments = ["up", repo]
    var environment = ProcessInfo.processInfo.environment
    environment["PATH"] = "/opt/homebrew/bin:/usr/local/bin:" + (environment["PATH"] ?? "/usr/bin:/bin")
    process.environment = environment
    do {
        try process.run()
    } catch {
        complain("Ganymede could not be brought up: \(error)")
        exit(1)
    }
    process.waitUntilExit()
    exit(process.terminationStatus)
}

final class TileDelegate: NSObject, NSApplicationDelegate {
    private var item: NSStatusItem?
    private var blockedItem, readyItem, workingItem: NSMenuItem!

    func applicationDidFinishLaunching(_ notification: Notification) {
        // The plist keeps LSUIElement, so the launcher role never flashes an
        // icon. Only the tile joins the Dock, and only for as long as the
        // Dashboard it belongs to is up.
        NSApp.setActivationPolicy(.regular)

        let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        item.menu = buildMenu()
        self.item = item
        show("0 0 0")

        // stdin is read off the main thread: the runloop has to stay free to
        // draw the surfaces this is about to change.
        DispatchQueue.global().async {
            while let line = readLine(strippingNewline: true) {
                DispatchQueue.main.async { self.show(line) }
            }
            DispatchQueue.main.async {
                NSApp.dockTile.badgeLabel = nil
                NSApp.terminate(nil)
            }
        }
    }

    // buildMenu is the dropdown a click on the status item now shows: an
    // explicit way to jump to Ghostty, since a click no longer does that by
    // itself, and the full Blocked/Ready/Working breakdown below it.
    //
    // The three count rows carry no action by design — they are read, not
    // clicked — but NSMenu auto-disables (and greys out) any item nothing in
    // the responder chain can act on, which is exactly the coloured counts
    // this menu is for. autoenablesItems turns that off.
    private func buildMenu() -> NSMenu {
        let menu = NSMenu()
        menu.autoenablesItems = false

        let open = menu.addItem(withTitle: "Open Ganymede", action: #selector(clicked), keyEquivalent: "")
        open.target = self
        menu.addItem(.separator())

        blockedItem = menu.addItem(withTitle: "", action: nil, keyEquivalent: "")
        blockedItem.isEnabled = true
        readyItem = menu.addItem(withTitle: "", action: nil, keyEquivalent: "")
        readyItem.isEnabled = true
        workingItem = menu.addItem(withTitle: "", action: nil, keyEquivalent: "")
        workingItem.isEnabled = true

        return menu
    }

    // show puts one line of counts on every surface. The Dock badge and the
    // menu-bar title still read Blocked alone, exactly as before — an empty
    // working set is a quiet one: no badge, and the menu bar back to saying
    // only that the harness is up. The dropdown's three rows read all of it.
    //
    // A line that is not three integers is left alone rather than guessed at.
    private func show(_ line: String) {
        let counts = line.split(separator: " ").compactMap { Int($0) }
        guard counts.count == 3 else { return }
        let (blocked, ready, working) = (counts[0], counts[1], counts[2])

        NSApp.dockTile.badgeLabel = blocked == 0 ? nil : String(blocked)
        let title: NSAttributedString
        if blocked == 0 {
            title = NSAttributedString(string: idleGlyph, attributes: [
                .font: NSFont.menuBarFont(ofSize: 0),
            ])
        } else {
            title = NSAttributedString(string: blockedGlyph + " " + String(blocked), attributes: [
                .font: NSFont.menuBarFont(ofSize: 0),
                .foregroundColor: blockedRed,
            ])
        }
        item?.button?.attributedTitle = title

        row(blockedItem, blockedGlyph, blocked, "Blocked", blockedRed)
        row(readyItem, readyGlyph, ready, "Ready", readyGreen)
        row(workingItem, workingGlyph, working, "Working", workingBlue)
    }

    // row sets one dropdown line: the tier's mark, count and name, in the
    // tier's own colour once there is one of it and the menu's ordinary text
    // colour otherwise — a zero row still names the tier, it is just not
    // something to look twice at.
    private func row(_ item: NSMenuItem, _ glyph: String, _ count: Int, _ name: String, _ colour: NSColor) {
        item.attributedTitle = NSAttributedString(string: "\(glyph) \(count) \(name)", attributes: [
            .foregroundColor: count == 0 ? NSColor.labelColor : colour,
        ])
    }

    @objc private func clicked() {
        forward()
    }

    // Clicking the Dock icon of an application with no windows: hand the
    // click on to the window the harness actually lives in.
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        forward()
        return false
    }
}

if CommandLine.arguments.contains("--tile") {
    let delegate = TileDelegate()
    NSApplication.shared.delegate = delegate
    NSApplication.shared.run()
} else {
    launch()
}
