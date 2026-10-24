module top (input clk, input [3:0] a, b, output logic [3:0] x, y, z);
  always_comb x = a & b;
  always @* begin
    if (a[0]) y = a; else y = b;
  end
  always_latch if (a[0]) z <= b;
  always_ff @(posedge clk) begin
    x <= a;
  end
endmodule
