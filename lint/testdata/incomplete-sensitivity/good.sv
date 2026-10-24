module top (input [3:0] a, b, c, input clk, output logic [3:0] x, y, z);
  localparam MASK = 4'hF;
  always @(a or b) x = (a & b) & MASK;
  always @(a, b, c) if (c[0]) y = a; else y = b;
  always @* z = a | b | c;
  always @(posedge clk) y <= a;
  always @(a or b) begin
    logic [3:0] t;
    t = a ^ b;
    x = t;
  end
  always @(a[0] or b) x = {3'b0, a[0]} & b;
endmodule
