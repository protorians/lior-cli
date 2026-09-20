# formula liorian — tap « jetbrains/lior-cli » (ce dépôt).
#
# Installation :
#   brew tap jetbrains/lior-cli https://github.com/jetbrains/lior-cli.git
#   brew install jetbrains/lior-cli/liorian
#
# Homebrew exige une référence à 3 segments (`user/repo/formula`) ; la forme
# `brew install jetbrains/lior-cli` (2 segments) n'est pas valide dans Homebrew.
# La formula porte le nom du binaire installé (`liorian`).
#
# Mise à jour (par release) : remplacer les `url`/`sha256` par plateforme depuis
# les assets GitHub (« lior-cli_<version>_<os>_<arch>.tar.gz ») et le
# `checksums.txt` de la release (Homebrew déduit `version` de l'URL). La date
# est la dernière release documentée.
class Liorian < Formula
  desc "Outil de dev pour creer, maintenir et publier des modules Liorian"
  homepage "https://github.com/jetbrains/lior-cli"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/jetbrains/lior-cli/releases/download/v0.15.0-alpha.1/lior-cli_0.15.0-alpha.1_darwin_arm64.tar.gz"
      sha256 "cd26a20ce355dfeeb275d838bb91e8f36f8aef01181715b7cf35710ec76babb2"
    else
      url "https://github.com/jetbrains/lior-cli/releases/download/v0.15.0-alpha.1/lior-cli_0.15.0-alpha.1_darwin_amd64.tar.gz"
      sha256 "b2f03e8af77c5259e20535699dd64c3da88f593224104d20e4fcc621e6916cce"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/jetbrains/lior-cli/releases/download/v0.15.0-alpha.1/lior-cli_0.15.0-alpha.1_linux_arm64.tar.gz"
      sha256 "437a168d3ab2b96d5fd5cb8004d2e0e47556d94e21d0f4c6f66cc1c9411f8efb"
    else
      url "https://github.com/jetbrains/lior-cli/releases/download/v0.15.0-alpha.1/lior-cli_0.15.0-alpha.1_linux_amd64.tar.gz"
      sha256 "909a7336413f7b636df06457ccd91bed36fa37e503d52300b60c291b932f89f4"
    end
  end

  def install
    bin.install "liorian"
  end

  def caveats
    <<~EOS
      Binaire pre-release non signe/notarise : si macOS bloque l'execution
      (" cannot be opened " / " killed "), appliquer :
        xattr -dr com.apple.quarantine #{bin}/liorian
    EOS
  end

  test do
    system bin/"liorian", "--version"
  end
end
